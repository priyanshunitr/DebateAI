package controllers

import "testing"

func TestIsAllowedGeneratedAvatarURL(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{url: "https://api.dicebear.com/9.x/big-ears/svg?seed=Jude", want: true},
		{url: "http://api.dicebear.com/9.x/big-ears/svg?seed=Jude", want: false},
		{url: "https://api.dicebear.com.evil.example/avatar", want: false},
		{url: "javascript:alert(1)", want: false},
	}

	for _, test := range tests {
		if got := isAllowedGeneratedAvatarURL(test.url); got != test.want {
			t.Errorf("isAllowedGeneratedAvatarURL(%q) = %v, want %v", test.url, got, test.want)
		}
	}
}
