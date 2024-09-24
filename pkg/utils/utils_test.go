package utils

import (
	"testing"
)

func TestGenHash(t *testing.T) {
	tests := []struct {
		name   string
		object interface{}
		want   string
	}{
		{
			name:   "Test with string",
			object: "test string",
			want:   "ee68a16fef8bf44a2b86b5614554b4079820f98dea14a67c3b507f59333cd591",
		},
		{
			name:   "Test with int",
			object: 12345,
			want:   "5994471abb01112afcc18159f6cc74b4f511b99806da59b3caf5a9c173cacfc5",
		},
		{
			name:   "Test with struct",
			object: struct{ Name string }{Name: "test"},
			want:   "3a7e9639e5a126efa16e6c730f9cbb141b0e7eac714e3c9388c7b308a146888c",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GenHash(tt.object); got != tt.want {
				t.Errorf("GenHash() = %v, want = %v", got, tt.want)
			}
		})
	}
}
