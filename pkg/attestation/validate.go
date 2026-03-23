package attestation

import (
	"fmt"

	"github.com/spiffe/go-spiffe/v2/bundle/jwtbundle"
	"github.com/spiffe/go-spiffe/v2/svid/jwtsvid"
)

// ValidateRemoteSVID validates a JWT-SVID from the remote side of a federation pair.
// It checks that:
//   - The token is a valid, signed JWT-SVID (signature, expiry)
//   - The token's audience includes the local trust domain name
//   - The token was issued by the expected remote trust domain
func ValidateRemoteSVID(token string, pair *FederationPair, bundles jwtbundle.Source) (*jwtsvid.SVID, error) {
	svid, err := jwtsvid.ParseAndValidate(token, bundles, []string{pair.Local.Name})
	if err != nil {
		return nil, fmt.Errorf("SVID validation failed: %w", err)
	}
	if svid.ID.TrustDomain().String() != pair.Remote.Name {
		return nil, fmt.Errorf(
			"SVID trust domain %q does not match expected remote %q",
			svid.ID.TrustDomain(), pair.Remote.Name,
		)
	}
	return svid, nil
}
