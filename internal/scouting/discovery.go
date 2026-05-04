package scouting

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// JWTClaims holds the subset of JWT payload fields this project needs.
// The signature is NOT verified; the token is only parsed to extract claims
// that the upstream API already vouched for.
type JWTClaims struct {
	Pgu string `json:"pgu"`
	UID int    `json:"uid"`
	Exp int64  `json:"exp"`
}

// ErrNoPack is returned when the profile has no organizationPositions whose
// unitType is "Pack".
var ErrNoPack = errors.New("no Pack organization position found in profile")

// ErrMultiplePacks is returned when the profile has more than one Pack
// organizationPosition. The wrapping error's message lists the unit numbers
// so the caller can disambiguate.
var ErrMultiplePacks = errors.New("multiple Pack organization positions found in profile")

// ParseJWT splits a JWT into its three segments, base64url-decodes the
// payload, and unmarshals it into JWTClaims. It does not verify the
// signature.
func ParseJWT(token string) (JWTClaims, error) {
	var claims JWTClaims

	if token == "" {
		return claims, errors.New("jwt: empty token")
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return claims, fmt.Errorf("jwt: expected 3 segments, got %d", len(parts))
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return claims, fmt.Errorf("jwt: decode payload: %w", err)
	}

	if err := json.Unmarshal(payload, &claims); err != nil {
		return claims, fmt.Errorf("jwt: unmarshal payload: %w", err)
	}

	return claims, nil
}

// DiscoverPackOrgGUID returns the organizationGuid of the single Pack-typed
// OrganizationPosition in the profile. It returns ErrNoPack when zero Packs
// are found and a wrapped ErrMultiplePacks (whose message lists the unit
// numbers) when more than one is found.
func DiscoverPackOrgGUID(profile PersonProfile) (string, error) {
	var packs []OrganizationPosition
	for _, op := range profile.OrganizationPositions {
		if op.UnitType == "Pack" {
			packs = append(packs, op)
		}
	}

	switch len(packs) {
	case 0:
		return "", ErrNoPack
	case 1:
		return packs[0].OrganizationGUID, nil
	default:
		numbers := make([]string, 0, len(packs))
		for _, p := range packs {
			numbers = append(numbers, p.UnitNumber)
		}
		return "", fmt.Errorf("%w: %s", ErrMultiplePacks, strings.Join(numbers, ", "))
	}
}
