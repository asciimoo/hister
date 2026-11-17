---
date: '2026-10-10T00:00:00+02:00'
draft: false
title: 'Language Settings and Detection'
description: 'Choose language detection settings, balance accuracy and memory use, preserve stop words, and apply changes to existing documents.'
---

Nogon detects document languages to help searches match related word forms in your content. You can choose which languages to consider, adjust memory use, and correct a document's language when needed.

The defaults suit most collections. Start with them unless short text, memory use, or phrase searches give you a reason to change a setting.

## Default Configuration

All four settings belong in the `indexer` section of your [configuration file](/docs/configuration):

```yaml
indexer:
  detect_languages: true
  languages: []
  language_detection_accuracy: 'low'
  keep_stopwords: false
```

| Setting                       | Default | Effect                                                                                                          |
| ----------------------------- | ------- | --------------------------------------------------------------------------------------------------------------- |
| `detect_languages`            | `true`  | Automatically identifies document languages. Set to `false` to turn detection off.                              |
| `languages`                   | `[]`    | An empty list considers all supported languages. A list must contain at least two distinct supported languages. |
| `language_detection_accuracy` | `low`   | `low` uses less memory; `high` can improve detection of short text.                                             |
| `keep_stopwords`              | `false` | Set to `true` to keep common words such as `for` and `your` searchable.                                         |

Configure these settings on the server and on any machine where you run service imports. They affect document search, not the interface language or the language a website serves.

## How Detection Affects Search

Nogon identifies a document's language from its text, without sending that text to an external service. For web pages with very little text, it also considers the title. A page's declared language can add regional information, such as American English, when it agrees with the text. Your manual language choice always takes precedence.

You can search across all languages without selecting one first. When Nogon cannot identify a language reliably, it labels the document `unknown`. The document remains searchable, though searches may not match different forms of the same word.

Each document has one language label. Mixed language pages, code, names, and very short snippets can be difficult to identify correctly. Support for matching related word forms also varies by language, even when the language is identified correctly.

## Choosing Settings

| Collection or priority              | Suggested settings                            | What to expect                                                                                                      |
| ----------------------------------- | --------------------------------------------- | ------------------------------------------------------------------------------------------------------------------- |
| General browsing across languages   | `languages: []`, accuracy `low`               | Considers all supported languages while keeping memory use lower.                                                   |
| Content in a known set of languages | A list such as `['en', 'de']`, accuracy `low` | Uses less memory, but content in other languages may receive an incorrect label.                                    |
| Short notes or snippets             | Accuracy `high`                               | Can improve detection of short text, at the cost of more memory. Very short or unclear text can still be `unknown`. |
| Minimal resource use                | `detect_languages: false`                     | Turns off automatic language detection for newly processed documents.                                               |
| Phrases containing common words     | `keep_stopwords: true`                        | Keeps words such as `for` and `your` searchable, using more storage.                                                |

You can use either accuracy mode with all supported languages or a restricted list. Preserving common words changes search matching, not detection accuracy.

## Accuracy and Memory

Use `low`, the default, to reduce memory use. Choose `high` if Nogon often misidentifies short documents in your collection:

```yaml
indexer:
  detect_languages: true
  languages: []
  language_detection_accuracy: 'high'
```

High accuracy can help with short text, but it does not guarantee a correct language for every document. Very short text may remain `unknown` in either mode.

Memory use depends on the content you process and the languages you allow. Limiting the language list can reduce it further. Restart Nogon after changing settings; memory use may not fall until you restart.

## Restricting Languages

Use `languages` when you know which languages your collection contains:

```yaml
indexer:
  detect_languages: true
  languages: ['en', 'de', 'nl', 'id']
  language_detection_accuracy: 'low'
```

Choose at least two distinct languages from the table below. Codes ignore letter case and surrounding whitespace. Regional codes count as the same language: `en-US` selects English, and `['en', 'en-GB']` does not count as two languages. Aliases also count as the same language, so `['no', 'nb']` is invalid.

This list controls automatic detection. It does not exclude documents from your collection or limit which languages you can search. To assign a single language to selected documents, use a manual language pin as described below.

### Confidence and Unknown Results

Nogon uses `unknown` when there is too little text or the language is unclear. This is more likely for short names, snippets, or mixed language content. A useful web page title can help when the body has too little text.

A restricted list makes Nogon more cautious about assigning a language, but documents in an excluded language can still receive the wrong label. Leave `languages` empty if your browsing spans unpredictable languages. Even listing every supported language explicitly can give different results from leaving the list empty.

### Supported Codes

Nogon can automatically detect these 30 languages. Use the code in the first column for new configuration; aliases are also accepted.

| Code | Language         | Aliases |
| ---- | ---------------- | ------- |
| `ar` | Arabic           |         |
| `bg` | Bulgarian        |         |
| `ca` | Catalan          |         |
| `da` | Danish           |         |
| `de` | German           |         |
| `el` | Greek            |         |
| `en` | English          |         |
| `es` | Spanish          |         |
| `eu` | Basque           |         |
| `fa` | Persian          |         |
| `fi` | Finnish          |         |
| `fr` | French           |         |
| `ga` | Irish            |         |
| `hi` | Hindi            |         |
| `hr` | Croatian         |         |
| `hu` | Hungarian        |         |
| `hy` | Armenian         |         |
| `id` | Indonesian       | `in`    |
| `it` | Italian          |         |
| `ja` | Japanese         |         |
| `ko` | Korean           |         |
| `nb` | Norwegian Bokmal | `no`    |
| `nl` | Dutch            |         |
| `pl` | Polish           |         |
| `pt` | Portuguese       |         |
| `ro` | Romanian         | `mo`    |
| `ru` | Russian          |         |
| `sv` | Swedish          |         |
| `tr` | Turkish          |         |
| `zh` | Chinese          |         |

Regional and script codes such as `en-US` and `zh-Hant` select English and Chinese for automatic detection. A page's declared language may supply a more specific label. Do not include `unknown` in the detection list.

Manual pins accept additional recognized language codes, such as `uk` for Ukrainian, even when automatic detection is unavailable. Use hyphens for regional codes, such as `en-US`. Private codes and codes with extensions are not accepted. Both `unknown` and `und` select the `unknown` label.

Manual pins also accept `iw` for Hebrew (`he`) and `ji` for Yiddish (`yi`). These languages are not available for automatic detection.

## Stop Words and Phrase Searches

Stop words are common words such as `for` and `your`. Nogon may omit them from searches by default. To keep them searchable:

```yaml
indexer:
  detect_languages: true
  keep_stopwords: true
```

This is useful for searches such as `text:"for your information"`, where common words are part of the phrase you want to match. Searches still allow the usual spelling and word form variations; quotes do not require an exact character for character match.

Preserving common words can increase storage use. Reindex existing documents after changing this setting so those words become searchable in previously saved content too.

## Environment Overrides

Environment variables override the corresponding YAML values. Separate languages with commas:

```bash
export NOGON__INDEXER__DETECT_LANGUAGES=true
export NOGON__INDEXER__LANGUAGES=en,de,nl,id
export NOGON__INDEXER__LANGUAGE_DETECTION_ACCURACY=low
export NOGON__INDEXER__KEEP_STOPWORDS=false
```

To consider all supported languages again, set `languages: []` in YAML and remove any `NOGON__INDEXER__LANGUAGES` or legacy `HISTER__INDEXER__LANGUAGES` override. See [Environment Variables](/docs/configuration#environment-variables) for configuration precedence.

## Applying Changes to Existing Documents

Restart the server after editing its configuration. Changes to detection settings apply when documents are next processed; they do not automatically change the languages of saved documents.

| Changed setting               | Action for existing documents                                                                                                               |
| ----------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| `languages`                   | Reindex to identify languages using the new list. Existing documents remain searchable, including those in languages removed from the list. |
| `language_detection_accuracy` | Reindex to use the new accuracy mode.                                                                                                       |
| `detect_languages`            | Reindex to apply the new detection setting. Existing documents remain searchable without reindexing.                                        |
| `keep_stopwords`              | Reindex to apply the new common word setting.                                                                                               |

After restarting with the desired settings, leave the server running and run:

```bash
nogon reindex
```

Use the same `detect_languages` setting for this command and the server. If you use a custom configuration file, pass it with `nogon --config /path/to/config.yml reindex`.

Turning off detection leaves existing documents searchable with their current language labels. Newly processed documents receive `unknown` unless you have pinned their language. Reindexing with detection disabled also changes unpinned documents to `unknown`. Manual pins are preserved.

Nogon may remind you to reindex after a configuration change, but it does not warn about every language setting. Use the table above to decide when to reindex. See [Data Storage and Lifecycle](/docs/data-lifecycle) for the other effects of reindexing.

## Checking and Correcting Languages

Use [search filters](/docs/query-language) to check document languages:

```textplain
language:en
language:en-US
language:(en|de)
language:unknown
```

Filters ignore letter case. `language:en` includes `en-US` and `en-GB`, while `language:en-US` selects American English. Both `language:nb` and `language:no` find Norwegian Bokmal. Older documents may display an alias until you update or reindex them.

`language:unknown` finds documents with no identified language, including those processed with detection disabled or manually set to `unknown`. It does not find every document in an unsupported or excluded language, since some may have been misidentified.

You can pin a document's language even when detection is disabled. For example, to mark documents from a known English site as English:

```bash
nogon update 'domain:english.example.com' --language en --dry
nogon update 'domain:english.example.com' --language en
```

The first command previews how many documents would change. The second asks for confirmation and applies the change. Use a regional code such as `--language en-US` when you know that distinction.

A pin takes effect without a full reindex and survives restarts, reindexing, imports, and document updates. It applies only to the selected documents and their owners. Adding the same page again does not replace your choice.

Use `--language unknown` to keep a document without an identified language. To clear a pin and identify the language again using the server's current settings:

```bash
nogon update 'domain:english.example.com' --language auto --dry
nogon update 'domain:english.example.com' --language auto
```

With detection disabled, `auto` clears the pin and sets the language to `unknown`. This command uses the saved content without downloading the page or rereading the source file. A dry run leaves documents unchanged.

If you corrected languages with an older Hister version before persistent pins were available, apply those choices again to keep them pinned.

Editing a web page's title can also change its detected language, since the title may help identify a page with very little text. Pinned languages stay the same.
