package main

import "testing"

func TestHavenMessageLooksLikeHostFolderOrganizeRequest(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{"please organize my files", true},
		{"Please organize my Downloads folder", true},
		{"can you organise the files on my desktop", true},
		{"tidy my downloads folder", true},
		{"hello", false},
		{"organize my thoughts", false},
		{"what is 2+2", false},
	}
	for _, tc := range cases {
		if got := havenMessageLooksLikeHostFolderOrganizeRequest(tc.msg); got != tc.want {
			t.Errorf("%q: got %v want %v", tc.msg, got, tc.want)
		}
	}
}
