package types

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"strings"
	"time"
)

const (
	RoleCreator  = "creator"
	RoleApprover = "approver"
	RoleProxy    = "proxy"
	RoleHost     = "host"
)

var ValidRoles = map[string]bool{
	RoleCreator:  true,
	RoleApprover: true,
	RoleProxy:    true,
	RoleHost:     true,
}

var ErrInvalidRole = errors.New("invalid role")
var ErrAuthorNotExist = errors.New("author does not exist on invoice")
var ErrSignatureKeyRoleMismatch = errors.New("signature key is not valid for the provided role")
var ErrInvalidSignatureKey = errors.New("signature key is not valid")
var ErrInvalidSignature = errors.New("signature is not valid")
var ErrMissingSignatureKey = errors.New("missing signature key")

// Cleartext format:
// Matt Butcher <matt.butcher@example.com>
// mybindle
// 0.1.0
// creator
// ~
// e1706ab0a39ac88094b6d54a3f5cdba41fe5a901
// 098fa798779ac88094b6d54a3f5cdba41fe5a901
// 5b992e90b71d5fadab3cd3777230ef370df75f5b

// NOTE: the spec (https://github.com/deislabs/bindle/blob/main/docs/signing-spec.md#signing-on-the-invoice)
// includes the `at` value in the cleartext, but the server does not, so this client does not either.
// Issue: https://github.com/deislabs/bindle/issues/284

// GenerateSignature generates a signature for the privided role and author,
// first validating that the given role is valid and the given author is included in the invoice
// and then appends it to the invoice's signature list
func (i *Invoice) GenerateSignature(author, role string, sigKey *SignatureKey, privKey []byte) error {
	if exists, val := ValidRoles[role]; !exists || !val {
		return ErrInvalidRole
	}

	if !sigKey.IncludesRole(role) {
		return ErrSignatureKeyRoleMismatch
	}

	if !i.IsAuthoredBy(author) {
		return ErrAuthorNotExist
	}

	timestamp := time.Now()

	cleartext := i.generateCleartext(author, role)

	sig := ed25519.Sign(privKey, []byte(cleartext))

	pubKey, err := base64.StdEncoding.DecodeString(sigKey.Key)
	if err != nil {
		return err
	}

	signature := Signature{
		By:        author,
		Signature: base64.StdEncoding.EncodeToString(sig),
		Key:       base64.StdEncoding.EncodeToString(pubKey),
		Role:      role,
		At:        timestamp.Unix(),
	}

	if i.Signature == nil {
		i.Signature = []Signature{}
	}

	i.Signature = append(i.Signature, signature)

	return nil
}

func (i *Invoice) VerifySignatures(sigKeys []SignatureKey) error {
	// map of author to key
	keys := map[string]*SignatureKey{}

	// first validate each key's signature and add them to a map
	for i := range sigKeys {
		key := sigKeys[i]

		keyBytes, err := base64.StdEncoding.DecodeString(key.Key)
		if err != nil {
			return err
		}

		labelSigBytes, err := base64.StdEncoding.DecodeString(key.LabelSignature)
		if err != nil {
			return err
		}

		if valid := ed25519.Verify(keyBytes, []byte(key.Label), labelSigBytes); !valid {
			return ErrInvalidSignatureKey
		}

		keys[key.Label] = &key
	}

	// then validate each of the invoice's signatures using the keys we have available
	for _, s := range i.Signature {
		key := keys[s.By]
		if key == nil {
			return ErrMissingSignatureKey
		}

		if !key.IncludesRole(s.Role) {
			return ErrSignatureKeyRoleMismatch
		}

		keyBytes, err := base64.StdEncoding.DecodeString(key.Key)
		if err != nil {
			return err
		}

		sigBytes, err := base64.StdEncoding.DecodeString(s.Signature)
		if err != nil {
			return err
		}

		cleartext := []byte(i.generateCleartext(key.Label, s.Role))

		if valid := ed25519.Verify(keyBytes, cleartext, sigBytes); !valid {
			return ErrInvalidSignature
		}
	}

	return nil
}

// IsAuthoredBy returns true if the provided author is in the
// list of authors for this invoice
func (i *Invoice) IsAuthoredBy(author string) bool {
	for _, a := range i.Bindle.Authors {
		if a == author {
			return true
		}
	}

	return false
}

func (i *Invoice) generateCleartext(author, role string) string {
	// metadata
	cleartextParts := []string{
		author,
		i.Bindle.Name,
		i.Bindle.Version,
		role,
		"~",
	}

	// parcel SHAs
	for _, p := range i.Parcel {
		cleartextParts = append(cleartextParts, p.Label.SHA256)
	}

	return strings.Join(cleartextParts, "\n")
}
