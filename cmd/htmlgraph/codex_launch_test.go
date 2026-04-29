package main

import (
	"testing"
)

// TestAppendOrReplaceEnv_AppendsNew verifies that a key not present in env is appended.
func TestAppendOrReplaceEnv_AppendsNew(t *testing.T) {
	env := []string{"FOO=bar", "BAZ=qux"}
	got := appendOrReplaceEnv(env, "NEW_KEY=new_value")
	found := false
	for _, kv := range got {
		if kv == "NEW_KEY=new_value" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected NEW_KEY=new_value to be appended; got %v", got)
	}
	// Original keys must still be present.
	for _, orig := range env {
		present := false
		for _, kv := range got {
			if kv == orig {
				present = true
				break
			}
		}
		if !present {
			t.Errorf("original key %q was lost; got %v", orig, got)
		}
	}
}

// TestAppendOrReplaceEnv_OverridesExisting verifies that an existing key is replaced in-place.
func TestAppendOrReplaceEnv_OverridesExisting(t *testing.T) {
	env := []string{"FOO=old", "BAR=keep"}
	got := appendOrReplaceEnv(env, "FOO=new")

	// FOO must be overridden.
	fooCount := 0
	for _, kv := range got {
		if kv == "FOO=old" {
			t.Errorf("stale FOO=old still present in %v", got)
		}
		if kv == "FOO=new" {
			fooCount++
		}
	}
	if fooCount != 1 {
		t.Errorf("expected exactly one FOO=new entry, got %d in %v", fooCount, got)
	}

	// BAR must be unchanged.
	barFound := false
	for _, kv := range got {
		if kv == "BAR=keep" {
			barFound = true
		}
	}
	if !barFound {
		t.Errorf("BAR=keep was lost; got %v", got)
	}
}

// TestAppendOrReplaceEnv_Multiple verifies that multiple kv pairs are all applied.
func TestAppendOrReplaceEnv_Multiple(t *testing.T) {
	env := []string{"EXISTING=old"}
	got := appendOrReplaceEnv(env, "EXISTING=new", "BRAND=fresh")

	existingNew := false
	brandFresh := false
	for _, kv := range got {
		if kv == "EXISTING=new" {
			existingNew = true
		}
		if kv == "BRAND=fresh" {
			brandFresh = true
		}
		if kv == "EXISTING=old" {
			t.Errorf("stale EXISTING=old still present in %v", got)
		}
	}
	if !existingNew {
		t.Errorf("EXISTING=new not found in %v", got)
	}
	if !brandFresh {
		t.Errorf("BRAND=fresh not found in %v", got)
	}
}

// TestAppendOrReplaceEnv_Empty verifies that empty input env is handled correctly.
func TestAppendOrReplaceEnv_Empty(t *testing.T) {
	got := appendOrReplaceEnv(nil, "KEY=val")
	if len(got) != 1 || got[0] != "KEY=val" {
		t.Errorf("expected [KEY=val], got %v", got)
	}

	got2 := appendOrReplaceEnv([]string{}, "A=1", "B=2")
	if len(got2) != 2 {
		t.Errorf("expected 2 entries, got %v", got2)
	}
}
