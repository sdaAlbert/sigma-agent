package sigmaagent

import (
	"reflect"
	"testing"
)

func TestGoTestArgs(t *testing.T) {
	tests := []struct {
		name    string
		profile string
		pkg     string
		want    []string
		wantErr bool
	}{
		{name: "whole module tests are bounded", profile: "go-test", want: []string{"go", "test", "-p", "2", "./..."}},
		{name: "focused package tests are bounded", profile: "go-test-package", pkg: "./core/mapping", want: []string{"go", "test", "-p", "2", "./core/mapping"}},
		{name: "vet is bounded", profile: "go-vet", want: []string{"go", "vet", "-p", "2", "./..."}},
		{name: "rejects parent path", profile: "go-test-package", pkg: "../outside", wantErr: true},
		{name: "rejects shell-like input", profile: "go-test-package", pkg: "./core;whoami", wantErr: true},
		{name: "requires package", profile: "go-test-package", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := goTestArgs(tt.profile, tt.pkg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("goTestArgs() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("goTestArgs() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
