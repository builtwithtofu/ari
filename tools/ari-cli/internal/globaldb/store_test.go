package globaldb

import (
	"context"
	"errors"
	"testing"
)

func TestSetMetaRoundTripUsesUpsertQuery(t *testing.T) {
	store := newMigratedGlobalDBStore(t, "meta-store")

	if err := store.SetMeta(context.Background(), "version", "0.3.0-dev"); err != nil {
		t.Fatalf("SetMeta returned error: %v", err)
	}
	if err := store.SetMeta(context.Background(), "version", "0.3.1-dev"); err != nil {
		t.Fatalf("SetMeta update returned error: %v", err)
	}
	value, err := store.GetMeta(context.Background(), "version")
	if err != nil {
		t.Fatalf("GetMeta returned error: %v", err)
	}
	if value != "0.3.1-dev" {
		t.Fatalf("GetMeta value = %q, want updated value", value)
	}
}

func TestGetMetaReturnsStoredValue(t *testing.T) {
	store := newMigratedGlobalDBStore(t, "meta-store")
	if err := store.SetMeta(context.Background(), "version", "0.3.0-dev"); err != nil {
		t.Fatalf("SetMeta returned error: %v", err)
	}

	value, err := store.GetMeta(context.Background(), "version")
	if err != nil {
		t.Fatalf("GetMeta returned error: %v", err)
	}
	if value != "0.3.0-dev" {
		t.Fatalf("GetMeta value = %q, want 0.3.0-dev", value)
	}
}

func TestGetMetaReturnsNotFoundSentinel(t *testing.T) {
	store := newMigratedGlobalDBStore(t, "meta-store")

	_, err := store.GetMeta(context.Background(), "missing")
	if err == nil {
		t.Fatal("GetMeta returned nil error for missing key")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetMeta error = %v, want ErrNotFound", err)
	}
}

func TestMetaMethodsRequireKey(t *testing.T) {
	store := newMigratedGlobalDBStore(t, "meta-store")

	_, err := store.GetMeta(context.Background(), "")
	if err == nil {
		t.Fatal("GetMeta returned nil error for empty key")
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("GetMeta error = %v, want ErrInvalidInput", err)
	}

	err = store.SetMeta(context.Background(), "", "value")
	if err == nil {
		t.Fatal("SetMeta returned nil error for empty key")
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("SetMeta error = %v, want ErrInvalidInput", err)
	}
}
