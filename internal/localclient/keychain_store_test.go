package localclient

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

const (
	keychainTestGrantA = "mp_lc1_pv_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	keychainTestGrantB = "mp_lc1_pv_BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
)

type fakeKeychainBackend struct {
	mu          sync.Mutex
	items       map[string][]byte
	err         error
	getCalls    int
	putCalls    int
	deleteCalls int
	putArgument []byte
	getResult   []byte
}

func newFakeKeychainBackend() *fakeKeychainBackend {
	return &fakeKeychainBackend{items: make(map[string][]byte)}
}

func (f *fakeKeychainBackend) get(service, account string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getCalls++
	if f.err != nil {
		return nil, f.err
	}
	value, ok := f.items[service+"\x00"+account]
	if !ok {
		return nil, errKeychainItemNotFound
	}
	f.getResult = bytes.Clone(value)
	return f.getResult, nil
}

func (f *fakeKeychainBackend) put(service, account string, value []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.putCalls++
	f.putArgument = value
	if f.err != nil {
		return f.err
	}
	key := service + "\x00" + account
	clear(f.items[key])
	f.items[key] = bytes.Clone(value)
	return nil
}

func (f *fakeKeychainBackend) delete(service, account string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleteCalls++
	if f.err != nil {
		return f.err
	}
	key := service + "\x00" + account
	value, ok := f.items[key]
	if !ok {
		return errKeychainItemNotFound
	}
	clear(value)
	delete(f.items, key)
	return nil
}

func keychainTestKey(binding string) CredentialKey {
	return CredentialKey{
		Environment: PreviewEnvironment,
		APIOrigin:   PreviewOrigin,
		WebOrigin:   PreviewOrigin,
		BindingID:   binding,
	}
}

func mustTestCredential(t *testing.T, raw string) Credential {
	t.Helper()
	credential, err := newCredential([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return credential
}

func TestKeychainStorePutGetUpdateDeleteWithFake(t *testing.T) {
	backend := newFakeKeychainBackend()
	store := newKeychainCredentialStore(backend)
	key := keychainTestKey("account_binding_aaaaaaaa")
	first := mustTestCredential(t, keychainTestGrantA)
	defer first.clear()
	if err := store.Put(context.Background(), key, first); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(backend.putArgument, []byte("mp_lc1")) || !allZero(backend.putArgument) {
		t.Fatal("Put did not zero its temporary credential copy")
	}

	got, err := store.Get(context.Background(), key)
	if err != nil || !got.valid() {
		t.Fatalf("Get = %v, %v", got, err)
	}
	got.clear()
	if !allZero(backend.getResult) {
		t.Fatal("Get did not zero the native result after cloning the credential")
	}

	second := mustTestCredential(t, keychainTestGrantB)
	defer second.clear()
	if err := store.Put(context.Background(), key, second); err != nil {
		t.Fatal(err)
	}
	updated, err := store.Get(context.Background(), key)
	if err != nil || string(updated.raw) != keychainTestGrantB {
		t.Fatalf("updated Get = %v, %v", updated, err)
	}
	updated.clear()
	if backend.putCalls != 2 {
		t.Fatalf("Put calls = %d", backend.putCalls)
	}

	if err := store.Delete(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(context.Background(), key); err != nil {
		t.Fatalf("idempotent Delete = %v", err)
	}
	if _, err := store.Get(context.Background(), key); !errors.Is(err, ErrCredentialMissing) {
		t.Fatalf("Get after Delete = %v", err)
	}
}

func TestKeychainStoreIsolatesAccountBindings(t *testing.T) {
	backend := newFakeKeychainBackend()
	store := newKeychainCredentialStore(backend)
	keyA := keychainTestKey("account_binding_aaaaaaaa")
	keyB := keychainTestKey("account_binding_bbbbbbbb")
	credential := mustTestCredential(t, keychainTestGrantA)
	defer credential.clear()
	if err := store.Put(context.Background(), keyA, credential); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(context.Background(), keyB); !errors.Is(err, ErrCredentialMissing) {
		t.Fatalf("account B read account A: %v", err)
	}
	accountA, _ := keychainAccount(keyA)
	accountB, _ := keychainAccount(keyB)
	if accountA == accountB || !strings.HasPrefix(accountA, "preview/") {
		t.Fatalf("unsafe account identities: %q %q", accountA, accountB)
	}
}

func TestMemoryCredentialStoreEnforcesExactOriginAndAccountIdentity(t *testing.T) {
	store := NewMemoryCredentialStore()
	key := keychainTestKey("account_binding_aaaaaaaa")
	credential := mustTestCredential(t, keychainTestGrantA)
	defer credential.clear()
	if err := store.Put(context.Background(), key, credential); err != nil {
		t.Fatal(err)
	}

	otherAccount := key
	otherAccount.BindingID = "account_binding_bbbbbbbb"
	if _, err := store.Get(context.Background(), otherAccount); !errors.Is(err, ErrCredentialMissing) {
		t.Fatalf("other account Get = %v", err)
	}
	alteredOrigin := key
	alteredOrigin.WebOrigin += "/"
	if _, err := store.Get(context.Background(), alteredOrigin); !errors.Is(err, ErrCredentialMissing) {
		t.Fatalf("altered origin Get = %v", err)
	}
	if err := store.Put(context.Background(), alteredOrigin, credential); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("altered origin Put = %v", err)
	}
}

func TestKeychainAccountUsesEveryExactBindingComponent(t *testing.T) {
	base := keychainTestKey("account_binding_aaaaaaaa")
	baseAccount, err := keychainAccount(base)
	if err != nil {
		t.Fatal(err)
	}
	const want = "preview/fc41cf8ec678983678aaa0be673402de"
	if baseAccount != want {
		t.Fatalf("account = %q, want %q", baseAccount, want)
	}

	for name, mutate := range map[string]func(*CredentialKey){
		"environment": func(k *CredentialKey) { k.Environment = "production" },
		"api origin":  func(k *CredentialKey) { k.APIOrigin += "/" },
		"web origin":  func(k *CredentialKey) { k.WebOrigin += "/" },
		"binding":     func(k *CredentialKey) { k.BindingID = "account_binding_bbbbbbbb" },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := base
			mutate(&candidate)
			account, err := keychainAccount(candidate)
			if name == "binding" {
				if err != nil || account == baseAccount {
					t.Fatalf("binding account = %q, %v", account, err)
				}
				return
			}
			if err == nil || account != "" {
				t.Fatalf("accepted altered %s: %q", name, account)
			}
		})
	}
}

func TestKeychainStoreRejectsHostileKeysBeforeBackend(t *testing.T) {
	for name, key := range map[string]CredentialKey{
		"empty":       {},
		"production":  {Environment: "production", APIOrigin: PreviewOrigin, WebOrigin: PreviewOrigin, BindingID: "account_binding_aaaaaaaa"},
		"api suffix":  {Environment: PreviewEnvironment, APIOrigin: PreviewOrigin + ".evil.test", WebOrigin: PreviewOrigin, BindingID: "account_binding_aaaaaaaa"},
		"web path":    {Environment: PreviewEnvironment, APIOrigin: PreviewOrigin, WebOrigin: PreviewOrigin + "/connect", BindingID: "account_binding_aaaaaaaa"},
		"bad binding": {Environment: PreviewEnvironment, APIOrigin: PreviewOrigin, WebOrigin: PreviewOrigin, BindingID: "../account"},
	} {
		t.Run(name, func(t *testing.T) {
			backend := newFakeKeychainBackend()
			store := newKeychainCredentialStore(backend)
			credential := mustTestCredential(t, keychainTestGrantA)
			defer credential.clear()
			if err := store.Put(context.Background(), key, credential); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("Put = %v", err)
			}
			if _, err := store.Get(context.Background(), key); !errors.Is(err, ErrCredentialMissing) {
				t.Fatalf("Get = %v", err)
			}
			if err := store.Delete(context.Background(), key); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("Delete = %v", err)
			}
			if backend.getCalls+backend.putCalls+backend.deleteCalls != 0 {
				t.Fatal("invalid identity reached native backend")
			}
		})
	}
}

func TestKeychainStoreMapsBackendErrorsWithoutSecretText(t *testing.T) {
	backend := newFakeKeychainBackend()
	backend.err = errors.New("hostile backend included " + keychainTestGrantA)
	store := newKeychainCredentialStore(backend)
	key := keychainTestKey("account_binding_aaaaaaaa")
	credential := mustTestCredential(t, keychainTestGrantA)
	defer credential.clear()
	for _, operation := range []func() error{
		func() error { return store.Put(context.Background(), key, credential) },
		func() error { _, err := store.Get(context.Background(), key); return err },
		func() error { return store.Delete(context.Background(), key) },
	} {
		err := operation()
		if !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), keychainTestGrantA) {
			t.Fatalf("unsafe mapped error: %v", err)
		}
	}
}

func TestKeychainStoreRejectsMalformedStoredCredential(t *testing.T) {
	backend := newFakeKeychainBackend()
	key := keychainTestKey("account_binding_aaaaaaaa")
	account, _ := keychainAccount(key)
	backend.items[keychainService+"\x00"+account] = []byte("not-a-grant")
	store := newKeychainCredentialStore(backend)
	if _, err := store.Get(context.Background(), key); !errors.Is(err, ErrCredentialMissing) {
		t.Fatalf("Get malformed item = %v", err)
	}
	if !allZero(backend.getResult) {
		t.Fatal("malformed native result was not zeroed")
	}
}

func TestKeychainStoreHonorsCanceledContextWithoutBackendCall(t *testing.T) {
	backend := newFakeKeychainBackend()
	store := newKeychainCredentialStore(backend)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	key := keychainTestKey("account_binding_aaaaaaaa")
	credential := mustTestCredential(t, keychainTestGrantA)
	defer credential.clear()
	if _, err := store.Get(ctx, key); !errors.Is(err, context.Canceled) {
		t.Fatalf("Get = %v", err)
	}
	if err := store.Put(ctx, key, credential); !errors.Is(err, context.Canceled) {
		t.Fatalf("Put = %v", err)
	}
	if err := store.Delete(ctx, key); !errors.Is(err, context.Canceled) {
		t.Fatalf("Delete = %v", err)
	}
	if backend.getCalls+backend.putCalls+backend.deleteCalls != 0 {
		t.Fatal("canceled operation reached native backend")
	}
}

func TestNilKeychainBackendFailsClosed(t *testing.T) {
	store := newKeychainCredentialStore(nil)
	if _, ok := store.(UnavailableCredentialStore); !ok {
		t.Fatalf("nil backend produced %T", store)
	}
}

func allZero(value []byte) bool {
	for _, b := range value {
		if b != 0 {
			return false
		}
	}
	return true
}
