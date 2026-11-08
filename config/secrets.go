// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
)

// References live only for this process and revision. They contain neither a
// credential nor a reversible encoding of one, and confer no authorization.
var settingReferenceKey = []byte(rand.Text())

type keptSecret struct {
	scope string
	value string
}

func secretScope(parts ...any) string {
	raw, _ := json.Marshal(parts)
	return string(raw)
}

func secretReference(revision, scope, value string) string {
	mac := hmac.New(sha256.New, settingReferenceKey)
	_, _ = mac.Write([]byte(secretScope(revision, scope, value)))
	return hex.EncodeToString(mac.Sum(nil))
}

// settingTree uses the configuration's public setting names for collection rows.
// It always detaches maps and slices from the active configuration.
func settingTree(value any) any {
	v := reflect.ValueOf(value)
	for v.IsValid() && (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) {
		v = v.Elem()
	}
	if !v.IsValid() {
		return nil
	}
	switch v.Kind() {
	case reflect.Struct:
		result := make(map[string]any)
		for field := range v.Type().Fields() {
			if name := field.Tag.Get("mapstructure"); name != "" && name != "-" {
				result[name] = settingTree(v.FieldByIndex(field.Index).Interface())
			}
		}
		return result
	case reflect.Map:
		result := make(map[string]any, v.Len())
		for iter := v.MapRange(); iter.Next(); {
			result[iter.Key().String()] = settingTree(iter.Value().Interface())
		}
		return result
	case reflect.Slice, reflect.Array:
		result := make([]any, 0, v.Len())
		for i := range v.Len() {
			result = append(result, settingTree(v.Index(i).Interface()))
		}
		return result
	default:
		return v.Interface()
	}
}

// mapSettingSecrets walks only described properties. Object scopes bind a kept
// secret to explicitly described public siblings, so reordering cookie rows works but changing a
// cookie's name, domain, or path requires an explicit replacement credential.
func mapSettingSecrets(schema SettingSchema, value any, scope string, transform func(any, string) (any, error)) (any, error) {
	if schema.Type == "string" && (schema.Sensitivity == "secret" || schema.Sensitivity == "url") {
		return transform(value, scope)
	}
	switch schema.Type {
	case "object", "dictionary":
		values, ok := value.(map[string]any)
		if !ok || values == nil {
			return nil, errors.New("use a collection with described properties")
		}
		properties := make(map[string]SettingSchema)
		public := make(map[string]any)
		for _, property := range schema.Properties {
			properties[property.Name] = property.Schema
			if slices.Contains(schema.SecretScope, property.Name) {
				if member, exists := values[property.Name]; exists {
					public[property.Name] = member
				}
			}
		}
		if schema.Type == "object" {
			scope = secretScope(scope, public)
		}
		result := make(map[string]any, len(values))
		for name, member := range values {
			child, ok := properties[name]
			if schema.Type == "dictionary" && schema.Items != nil {
				child, ok = *schema.Items, true
			}
			if !ok {
				return nil, errors.New("this collection contains undescribed or file managed options")
			}
			mapped, err := mapSettingSecrets(child, member, secretScope(scope, name), transform)
			if err != nil {
				return nil, err
			}
			result[name] = mapped
		}
		return result, nil
	case "list":
		values, ok := value.([]any)
		if !ok || schema.Items == nil {
			return nil, errors.New("use a list with described items")
		}
		result := make([]any, 0, len(values))
		for _, member := range values {
			mapped, err := mapSettingSecrets(*schema.Items, member, scope, transform)
			if err != nil {
				return nil, err
			}
			result = append(result, mapped)
		}
		return result, nil
	default:
		return value, nil
	}
}

func (c *Config) secretSettingState(field FieldMetadata, revision string) (any, map[string]keptSecret, error) {
	refs := make(map[string]keptSecret)
	value, err := mapSettingSecrets(field.Schema, settingTree(settingValue(c, field.Path)), field.Path, func(value any, scope string) (any, error) {
		text, ok := value.(string)
		if !ok {
			return nil, errors.New("the stored secret has an unsupported type")
		}
		if text == "" {
			return map[string]any{"action": "clear"}, nil
		}
		ref := secretReference(revision, scope, text)
		refs[ref] = keptSecret{scope: scope, value: text}
		result := map[string]any{"action": "keep"}
		if revision != "" {
			result["ref"] = ref
		}
		return result, nil
	})
	return value, refs, err
}

func (c *Config) resolveSettingSecrets(field FieldMetadata, revision string, value any) (any, error) {
	_, refs, _ := c.secretSettingState(field, revision)
	return mapSettingSecrets(field.Schema, value, field.Path, func(value any, scope string) (any, error) {
		invalid := errors.New("use Keep with a current reference, Replace with a value, or Clear; kept credentials must retain their destination and name")
		action, ok := value.(map[string]any)
		if !ok {
			return nil, invalid
		}
		switch action["action"] {
		case "keep":
			ref, _ := action["ref"].(string)
			kept, exists := refs[ref]
			if len(action) != 2 || revision == "" || !exists || kept.scope != scope {
				return nil, invalid
			}
			return kept.value, nil
		case "replace":
			text, ok := action["value"].(string)
			if !ok || !validSecretActionKeys(action, "action", "value", "ref") {
				return nil, invalid
			}
			return text, nil
		case "clear":
			if !validSecretActionKeys(action, "action", "ref") {
				return nil, invalid
			}
			return "", nil
		default:
			return nil, invalid
		}
	})
}

// A replacement or clear may retain the opaque reference so the browser can
// switch back to Keep before saving. It is never used to resolve those actions.
func validSecretActionKeys(action map[string]any, allowed ...string) bool {
	for key := range action {
		if !slices.Contains(allowed, key) {
			return false
		}
	}
	if ref, ok := action["ref"]; ok {
		if _, ok := ref.(string); !ok {
			return false
		}
	}
	return true
}
