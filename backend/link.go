package main

// linkAction is what to do when an external identity signs in.
type linkAction int

const (
	linkLogin    linkAction = iota // identity already belongs to a user
	linkCreate                     // no account uses this email: create one
	linkAttach                     // verified account: attach, keep its password
	linkTakeover                   // unverified account: attach, drop password, revoke sessions
	linkRefuse                     // provider does not vouch for a clashing email
)

func (a linkAction) String() string {
	return [...]string{"login", "create", "attach", "takeover", "refuse"}[a]
}

// emailOwner is the existing account, if any, that already uses the email an
// external identity presents.
type emailOwner struct {
	userID        string
	emailVerified bool
}

// decideLink holds the account-linking policy. It is pure so both stores share
// one tested implementation and differ only in how they apply the result.
//
//	identity linked?  account with that email   provider vouches?   result
//	────────────────  ────────────────────────  ─────────────────   ────────
//	yes               (any)                     (any)               login
//	no                none                      (any)               create
//	no                verified                  yes                 attach
//	no                unverified                yes                 takeover
//	no                any                       no                  refuse
//
// Takeover exists because anyone can register a password account with an
// email they don't own. If the real owner then signs in through a provider
// that proves ownership, merging and keeping the password would leave them in
// an account whose password a stranger may know ("pre-account hijacking").
// So the provider wins: the unproven password is removed and every existing
// session is ended. An honest user loses only a password they can set again.
func decideLink(alreadyLinked bool, owner *emailOwner, providerVerified bool) linkAction {
	switch {
	case alreadyLinked:
		return linkLogin
	case owner == nil:
		return linkCreate
	case !providerVerified:
		return linkRefuse
	case owner.emailVerified:
		return linkAttach
	default:
		return linkTakeover
	}
}
