package main

import "testing"

func TestDecideLink(t *testing.T) {
	verified := &emailOwner{userID: "u", emailVerified: true}
	unverified := &emailOwner{userID: "u", emailVerified: false}

	cases := []struct {
		name             string
		alreadyLinked    bool
		owner            *emailOwner
		providerVerified bool
		want             linkAction
	}{
		{"returning identity", true, nil, true, linkLogin},
		{"returning identity even if email now clashes", true, verified, false, linkLogin},
		{"new identity, email unused", false, nil, true, linkCreate},
		{"new identity, unverified email unused", false, nil, false, linkCreate},
		{"verified account, provider vouches", false, verified, true, linkAttach},
		{"unverified account, provider vouches", false, unverified, true, linkTakeover},
		{"verified account, provider does not vouch", false, verified, false, linkRefuse},
		{"unverified account, provider does not vouch", false, unverified, false, linkRefuse},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := decideLink(c.alreadyLinked, c.owner, c.providerVerified); got != c.want {
				t.Errorf("decideLink = %v, want %v", got, c.want)
			}
		})
	}
}
