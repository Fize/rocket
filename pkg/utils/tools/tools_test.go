package tools

import (
	"crypto/sha1"
	"fmt"
	"strings"
	"testing"
)

func TestGenerateName(t *testing.T) {
	type args struct {
		prefix string
		name   string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "normal case",
			args: args{
				prefix: "test-prefix",
				name:   "test-name",
			},
			want: "test-prefix-test-name",
		},
		{
			name: "empty prefix",
			args: args{
				prefix: "",
				name:   "test-name",
			},
			want: "-test-name",
		},
		{
			name: "empty name",
			args: args{
				prefix: "test-prefix",
				name:   "",
			},
			want: "test-prefix-",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GenerateName(tt.args.prefix, tt.args.name); got != tt.want {
				t.Errorf("GenerateName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGenerateNameWithHash(t *testing.T) {
	type args struct {
		prefix string
		name   string
	}
	tests := []struct {
		name string
		args args
		want func(string) bool // check if the hash is valid
	}{
		{
			name: "normal case",
			args: args{
				prefix: "test-prefix",
				name:   "test-name",
			},
			want: func(s string) bool {
				// Format: prefix-name-hash
				expectedPrefix := "test-prefix-test-name-"
				if !strings.HasPrefix(s, expectedPrefix) {
					return false
				}
				hashPart := strings.TrimPrefix(s, expectedPrefix)
				if len(hashPart) != 10 {
					return false
				}
				// Verify hash correctness
				n := "test-prefix-test-name"
				h := sha1.New()
				h.Write([]byte(n))
				fullHash := fmt.Sprintf("%x", h.Sum(nil))
				return hashPart == fullHash[:10]
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GenerateNameWithHash(tt.args.prefix, tt.args.name)
			if !tt.want(got) {
				t.Errorf("GenerateNameWithHash() = %v, check failed", got)
			}
		})
	}
}

type mockKMetadata struct {
	name      string
	namespace string
}

func (m *mockKMetadata) GetName() string {
	return m.name
}

func (m *mockKMetadata) GetNamespace() string {
	return m.namespace
}

func TestKObj(t *testing.T) {
	type args struct {
		obj KMetadata
	}
	tests := []struct {
		name string
		args args
		want ObjectRef
	}{
		{
			name: "normal object",
			args: args{
				obj: &mockKMetadata{name: "test-name", namespace: "test-ns"},
			},
			want: ObjectRef{Name: "test-name", Namespace: "test-ns"},
		},
		{
			name: "nil object",
			args: args{
				obj: nil,
			},
			want: ObjectRef{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := KObj(tt.args.obj); got != tt.want {
				t.Errorf("KObj() = %v, want %v", got, tt.want)
			}
		})
	}
}
