// SPDX-License-Identifier: AGPL-3.0-or-later

package model_test

import (
	"errors"
	"testing"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/model"
	"github.com/asciimoo/hister/server/testutil"
)

func TestOIDCRequiresIndependentRecoveryAdministrator(t *testing.T) {
	cfg := testutil.Config(t)
	cfg.App.UserHandling = true
	cfg.Server.OAuthOnly = true
	cfg.Server.OAuth = map[string]*config.OAuthEntry{"oidc": {AdminGroups: []string{"owners"}}}
	testutil.InitModelWithConfig(t, cfg)
	providerUser, err := model.CreateOAuthUser("provider", "oidc-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := model.SyncOAuthAdmin(providerUser.OAuthID, true); err != nil {
		t.Fatal(err)
	}
	if err := model.CheckAdministratorAvailability(cfg); err == nil {
		t.Fatal("OIDC administrator alone cannot guarantee recovery after role revocation")
	}
	local, err := model.CreateUser("recovery", "password123", true)
	if err != nil {
		t.Fatal(err)
	}
	// Password login is disabled, but the offline recovery token remains usable.
	if err := model.CheckAdministratorAvailability(cfg); err != nil {
		t.Fatal(err)
	}
	no := false
	for _, remove := range []func() error{
		func() error { return model.CheckUserDeletion(local.ID) },
		func() error { return model.DeleteUserByID(local.ID) },
		func() error { return model.DeleteUser(local.Username) },
		func() error { _, err := model.ToggleAdmin(local.Username); return err },
		func() error {
			_, err := model.UpdateUser(local.ID, local.ID, local.Revision, model.UserChanges{IsAdmin: &no})
			return err
		},
	} {
		if err := remove(); err == nil {
			t.Fatal("removed the independent recovery administrator")
		}
	}
	if user, err := model.SyncOAuthAdmin(providerUser.OAuthID, false); err != nil || user.IsAdmin {
		t.Fatalf("provider revocation was not authoritative: %v", err)
	}
	if _, err := model.GetUserByToken(local.Token); err != nil {
		t.Fatalf("recovery credential no longer works: %v", err)
	}
}

func TestConcurrentRecoveryAdministratorRemoval(t *testing.T) {
	cfg := testutil.Config(t)
	cfg.App.UserHandling = true
	cfg.Server.OAuth = map[string]*config.OAuthEntry{"oidc": {AdminGroups: []string{"owners"}}}
	testutil.InitModelWithConfig(t, cfg)
	for _, name := range []string{"first", "second"} {
		if _, err := model.CreateUser(name, "password123", true); err != nil {
			t.Fatal(err)
		}
	}
	provider, err := model.CreateOAuthUser("provider", "oidc-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := model.SyncOAuthAdmin(provider.OAuthID, true); err != nil {
		t.Fatal(err)
	}
	start, results := make(chan struct{}), make(chan error, 2)
	go func() { <-start; results <- model.DeleteUser("first") }()
	go func() { <-start; _, err := model.ToggleAdmin("second"); results <- err }()
	close(start)
	var success, blocked int
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			success++
		case errors.Is(err, model.ErrLastRecoveryAdmin):
			blocked++
		default:
			t.Fatalf("unexpected removal error: %v", err)
		}
	}
	if success != 1 || blocked != 1 {
		t.Fatalf("success = %d, blocked = %d", success, blocked)
	}
	if _, err := model.SyncOAuthAdmin(provider.OAuthID, false); err != nil {
		t.Fatal(err)
	}
	if err := model.CheckAdministratorAvailability(cfg); err != nil {
		t.Fatalf("concurrent mutations lost recovery access: %v", err)
	}
}

func TestAdministratorAvailabilityRequiresUsableCredential(t *testing.T) {
	cfg := testutil.InitModel(t)
	cfg.App.UserHandling = true
	admin, err := model.CreateUser("owner", "password123", true)
	if err != nil {
		t.Fatal(err)
	}
	// Represent an existing account whose only credential is its password.
	if err := model.DB.Model(admin).Update("token", "").Error; err != nil {
		t.Fatal(err)
	}
	if err := model.CheckAdministratorAvailability(cfg); err != nil {
		t.Fatal(err)
	}
	cfg.Server.OAuthOnly = true
	if err := model.CheckAdministratorAvailability(cfg); err == nil {
		t.Fatal("a role flag alone allowed disabling the administrator's only credential")
	}
}

func TestUpdatePassword(t *testing.T) {
	testutil.InitModel(t)
	testutil.CreateUser(t, "alice")

	if err := model.UpdatePassword("alice", "newpassword123"); err != nil {
		t.Fatal(err)
	}
	if _, err := model.AuthenticateUser("alice", "password123"); !errors.Is(err, model.ErrInvalidPassword) {
		t.Fatalf("old password authentication error = %v, want %v", err, model.ErrInvalidPassword)
	}
	if _, err := model.AuthenticateUser("alice", "newpassword123"); err != nil {
		t.Fatalf("new password authentication failed: %v", err)
	}

	user, err := model.GetUser("alice")
	if err != nil {
		t.Fatal(err)
	}
	if user.Password == "newpassword123" {
		t.Fatal("updated password was stored as plain text")
	}
}

func TestUpdatePasswordUserNotFound(t *testing.T) {
	testutil.InitModel(t)

	err := model.UpdatePassword("missing", "newpassword123")
	if !errors.Is(err, model.ErrUserNotFound) {
		t.Fatalf("UpdatePassword error = %v, want %v", err, model.ErrUserNotFound)
	}
}

func TestDeleteUserByIDAfterRename(t *testing.T) {
	testutil.InitModel(t)
	original := testutil.CreateUser(t, "alice")
	if err := model.UpdateUsername("alice", "renamed"); err != nil {
		t.Fatal(err)
	}
	replacement := testutil.CreateUser(t, "alice")
	if err := model.DeleteUserByID(original.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := model.GetUserByID(original.ID); !errors.Is(err, model.ErrUserNotFound) {
		t.Fatalf("original user lookup = %v, want user not found", err)
	}
	if _, err := model.GetUserByID(replacement.ID); err != nil {
		t.Fatalf("deletion affected the replacement account: %v", err)
	}
	for _, id := range []uint{0, original.ID, replacement.ID + 1} {
		if err := model.DeleteUserByID(id); !errors.Is(err, model.ErrUserNotFound) {
			t.Errorf("DeleteUserByID(%d) = %v, want user not found", id, err)
		}
	}
}

func TestGetUserByIDDatabaseError(t *testing.T) {
	testutil.InitModel(t)
	user := testutil.CreateUser(t, "alice")
	db, err := model.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := model.GetUserByID(user.ID); err == nil || errors.Is(err, model.ErrUserNotFound) {
		t.Fatalf("database failure reported as missing user: %v", err)
	}
}

func TestCreateUserConcurrentDuplicate(t *testing.T) {
	testutil.InitModel(t)
	db, err := model.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	// Keep the in-memory SQLite database on a single connection.
	db.SetMaxOpenConns(1)
	const attempts = 4
	start := make(chan struct{})
	results := make(chan error, attempts)
	for range attempts {
		go func() {
			<-start
			_, err := model.CreateUser("alice", "password123", false)
			results <- err
		}()
	}
	close(start)
	created := 0
	for range attempts {
		err := <-results
		if err == nil {
			created++
		} else if !errors.Is(err, model.ErrUserAlreadyExists) {
			t.Errorf("duplicate creation error = %v, want %v", err, model.ErrUserAlreadyExists)
		}
	}
	if created != 1 {
		t.Fatalf("created %d accounts, want 1", created)
	}
	if _, err := model.AuthenticateUser("alice", "password123"); err != nil {
		t.Fatalf("created account cannot authenticate: %v", err)
	}
}

func TestUserRevisionsTrackAdministrativeChanges(t *testing.T) {
	testutil.InitModel(t)
	admin, err := model.CreateUser("admin", "password123", true)
	if err != nil {
		t.Fatal(err)
	}
	user := testutil.CreateUser(t, "alice")
	changes := []func() error{
		func() error { return model.UpdateUsername("alice", "renamed") },
		func() error { return model.UpdatePassword("renamed", "replacement123") },
		func() error { _, err := model.RegenerateToken(user.ID); return err },
		func() error { _, err := model.ToggleAdmin("renamed"); return err },
	}
	for _, change := range changes {
		if err := change(); err != nil {
			t.Fatal(err)
		}
		if _, err := model.UpdateUser(admin.ID, user.ID, user.Revision, model.UserChanges{RegenerateToken: true}); !errors.Is(err, model.ErrStaleUserRevision) {
			t.Fatalf("outdated update was not rejected: %v", err)
		}
		current, err := model.GetUserByID(user.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.Revision != user.Revision+1 {
			t.Fatalf("revision = %d, want %d", current.Revision, user.Revision+1)
		}
		user = current
	}
}

func TestSyncOAuthAdminAccountScope(t *testing.T) {
	testutil.InitModel(t)
	admin, err := model.CreateUser("admin", "password123", true)
	if err != nil {
		t.Fatal(err)
	}
	user, err := model.CreateOAuthUser("alice", "oidc-alice@example.com")
	if err != nil {
		t.Fatal(err)
	}
	for _, identity := range []string{"", "missing", "alice", "admin"} {
		if _, err := model.SyncOAuthAdmin(identity, false); !errors.Is(err, model.ErrUserNotFound) {
			t.Fatalf("unlinked identity %q: %v", identity, err)
		}
	}
	if current, err := model.GetUserByID(admin.ID); err != nil || !current.IsAdmin || current.Revision != admin.Revision {
		t.Fatalf("unlinked identity changed the local administrator: %+v, %v", current, err)
	}
	if _, err := model.SyncOAuthAdmin(user.OAuthID, true); err != nil {
		t.Fatal(err)
	}
	no := false
	if _, err := model.UpdateUser(admin.ID, user.ID, user.Revision, model.UserChanges{IsAdmin: &no}); !errors.Is(err, model.ErrStaleUserRevision) {
		t.Fatalf("stale administrative update overwrote synchronized role: %v", err)
	}
}

func TestConcurrentUserUpdates(t *testing.T) {
	// A real database and multiple connections exercise database locking, rather
	// than hiding a race behind an in-memory database with one connection.
	testutil.InitModelWithConfig(t, testutil.Config(t))
	admin, err := model.CreateUser("admin", "password123", true)
	if err != nil {
		t.Fatal(err)
	}
	user := testutil.CreateUser(t, "alice")
	start, results := make(chan struct{}), make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			_, err := model.UpdateUser(admin.ID, user.ID, user.Revision, model.UserChanges{RegenerateToken: true})
			results <- err
		}()
	}
	close(start)
	var success, stale int
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			success++
		case errors.Is(err, model.ErrStaleUserRevision):
			stale++
		default:
			t.Errorf("concurrent update failed unexpectedly: %v", err)
		}
	}
	if success != 1 || stale != 1 {
		t.Fatalf("success = %d, stale = %d", success, stale)
	}
}

func TestConcurrentLastAdminRemoval(t *testing.T) {
	for _, otherOperation := range []string{"patch", "toggle", "delete", "delete by ID"} {
		t.Run(otherOperation, func(t *testing.T) {
			testutil.InitModelWithConfig(t, testutil.Config(t))
			first, err := model.CreateUser("first", "password123", true)
			if err != nil {
				t.Fatal(err)
			}
			second, err := model.CreateUser("second", "password123", true)
			if err != nil {
				t.Fatal(err)
			}
			start, results := make(chan struct{}), make(chan error, 2)
			go func() {
				<-start
				no := false
				_, err := model.UpdateUser(first.ID, first.ID, first.Revision, model.UserChanges{IsAdmin: &no})
				results <- err
			}()
			go func() {
				<-start
				var err error
				switch otherOperation {
				case "patch":
					no := false
					_, err = model.UpdateUser(second.ID, second.ID, second.Revision, model.UserChanges{IsAdmin: &no})
				case "toggle":
					_, err = model.ToggleAdmin(second.Username)
				case "delete":
					err = model.DeleteUser(second.Username)
				case "delete by ID":
					err = model.DeleteUserByID(second.ID)
				}
				results <- err
			}()
			close(start)
			var success, blocked int
			for range 2 {
				err := <-results
				switch {
				case err == nil:
					success++
				case errors.Is(err, model.ErrLastAdmin):
					blocked++
				default:
					t.Errorf("unexpected removal error: %v", err)
				}
			}
			var admins int64
			if err := model.DB.Model(&model.User{}).Where("is_admin = ?", true).Count(&admins).Error; err != nil {
				t.Fatal(err)
			}
			if success != 1 || blocked != 1 || admins != 1 {
				t.Fatalf("success = %d, blocked = %d, administrators = %d", success, blocked, admins)
			}
		})
	}
}

func TestUpdateUserRechecksAdministrator(t *testing.T) {
	testutil.InitModel(t)
	admin, err := model.CreateUser("admin", "password123", true)
	if err != nil {
		t.Fatal(err)
	}
	user := testutil.CreateUser(t, "alice")
	for _, actorID := range []uint{0, user.ID, user.ID + 1} {
		if _, err := model.UpdateUser(actorID, admin.ID, admin.Revision, model.UserChanges{RegenerateToken: true}); !errors.Is(err, model.ErrAdminRequired) {
			t.Fatalf("unauthorized actor %d update: %v", actorID, err)
		}
	}
}

func TestTokenRotationOwnership(t *testing.T) {
	testutil.InitModel(t)
	admin, err := model.CreateUser("admin", "password123", true)
	if err != nil {
		t.Fatal(err)
	}
	alice := testutil.CreateUser(t, "alice")
	bob := testutil.CreateUser(t, "bob")
	for _, actor := range []uint{0, bob.ID, bob.ID + 1} {
		if _, err := model.RotateUserToken(actor, alice.ID, 1); !errors.Is(err, model.ErrAdminRequired) {
			t.Fatalf("unauthorized rotation: %v", err)
		}
	}
	if _, err := model.UpdateUser(alice.ID, alice.ID, 1, model.UserChanges{RegenerateToken: true}); !errors.Is(err, model.ErrAdminRequired) {
		t.Fatalf("self service weakened administrative updates: %v", err)
	}
	rotated, err := model.RotateUserToken(alice.ID, alice.ID, 1)
	if err != nil || rotated.Revision != 2 || rotated.Token == alice.Token {
		t.Fatalf("self rotation: %v", err)
	}
	if _, err := model.RotateUserToken(alice.ID, alice.ID, 1); !errors.Is(err, model.ErrStaleUserRevision) {
		t.Fatalf("stale self rotation: %v", err)
	}
	rotated, err = model.RotateUserToken(admin.ID, alice.ID, 2)
	if err != nil || rotated.Revision != 3 {
		t.Fatalf("admin rotation: %v", err)
	}
	if _, err := model.ToggleAdmin("bob"); err != nil {
		t.Fatal(err)
	}
	if _, err := model.ToggleAdmin("admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := model.RotateUserToken(admin.ID, alice.ID, 3); !errors.Is(err, model.ErrAdminRequired) {
		t.Fatalf("demoted administrator still rotated someone else's token: %v", err)
	}
}
