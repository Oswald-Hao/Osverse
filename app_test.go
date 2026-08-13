package main

import "testing"

func TestNewApp(t *testing.T) {
	if NewApp() == nil {
		t.Fatal("NewApp() returned nil")
	}
}
