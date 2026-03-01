package configstore

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/url"
	"regexp"
)

var tenantIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{2,62}$`)
var keyIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{2,62}$`)

var validAlgorithms = map[string]bool{
	"ES256": true,
	"RS256": true,
	"EdDSA": true,
}

// Validate checks a ConfigFile for completeness and correctness.
// Returns errors.Join with all failures found.
func Validate(cfg *ConfigFile) error {
	var errs []error

	if len(cfg.Tenants) == 0 {
		errs = append(errs, errors.New("config must define at least one tenant"))
		return errors.Join(errs...)
	}

	tenantIDs := make(map[string]bool)
	keyIDs := make(map[string]bool)
	issuerURLs := make(map[string]string) // issuer URL → tenant ID

	for i, t := range cfg.Tenants {
		prefix := fmt.Sprintf("tenants[%d](%s)", i, t.ID)

		// Validate tenant ID format
		if t.ID == "" {
			errs = append(errs, fmt.Errorf("%s: id is required", prefix))
		} else if !tenantIDPattern.MatchString(t.ID) {
			errs = append(errs, fmt.Errorf("%s: id must match ^[a-z][a-z0-9-]{2,62}$", prefix))
		}

		// Unique tenant IDs
		if tenantIDs[t.ID] {
			errs = append(errs, fmt.Errorf("%s: duplicate tenant id", prefix))
		}
		tenantIDs[t.ID] = true

		// Validate name
		if t.Name == "" {
			errs = append(errs, fmt.Errorf("%s: name is required", prefix))
		}

		// Validate consumer type
		if t.ConsumerType != "" && t.ConsumerType != "shared" && t.ConsumerType != "dedicated" {
			errs = append(errs, fmt.Errorf("%s: consumer_type must be 'shared' or 'dedicated'", prefix))
		}

		// At least one category
		if len(t.Categories) == 0 {
			errs = append(errs, fmt.Errorf("%s: at least one category is required", prefix))
		}
		for j, cat := range t.Categories {
			if cat.Name == "" {
				errs = append(errs, fmt.Errorf("%s.categories[%d]: name is required", prefix, j))
			}
		}

		// Validate keys
		for j, key := range t.Keys {
			keyPrefix := fmt.Sprintf("%s.keys[%d](%s)", prefix, j, key.ID)

			if key.ID == "" {
				errs = append(errs, fmt.Errorf("%s: id is required", keyPrefix))
			} else if !keyIDPattern.MatchString(key.ID) {
				errs = append(errs, fmt.Errorf("%s: id must match ^[a-z][a-z0-9-]{2,62}$", keyPrefix))
			}

			// Unique key IDs across all tenants
			if keyIDs[key.ID] {
				errs = append(errs, fmt.Errorf("%s: duplicate key id across tenants", keyPrefix))
			}
			keyIDs[key.ID] = true

			// Algorithm validation
			if !validAlgorithms[key.Algorithm] {
				errs = append(errs, fmt.Errorf("%s: algorithm must be ES256, RS256, or EdDSA", keyPrefix))
			}

			// PEM format validation
			if key.PublicKey == "" {
				errs = append(errs, fmt.Errorf("%s: public_key is required", keyPrefix))
			} else if err := validatePEM(key.PublicKey); err != nil {
				errs = append(errs, fmt.Errorf("%s: invalid public_key PEM: %w", keyPrefix, err))
			}
		}

		// Validate OIDC
		if t.OIDC != nil {
			oidcPrefix := fmt.Sprintf("%s.oidc", prefix)

			if t.OIDC.IssuerURL == "" {
				errs = append(errs, fmt.Errorf("%s: issuer_url is required", oidcPrefix))
			} else {
				if err := validateHTTPSURL(t.OIDC.IssuerURL); err != nil {
					errs = append(errs, fmt.Errorf("%s: issuer_url: %w", oidcPrefix, err))
				}

				// No conflicting OIDC issuers across tenants
				if existingTenant, ok := issuerURLs[t.OIDC.IssuerURL]; ok {
					errs = append(errs, fmt.Errorf("%s: issuer_url %q already used by tenant %q", oidcPrefix, t.OIDC.IssuerURL, existingTenant))
				}
				issuerURLs[t.OIDC.IssuerURL] = t.ID
			}

			if t.OIDC.JWKSURL != "" {
				if err := validateHTTPSURL(t.OIDC.JWKSURL); err != nil {
					errs = append(errs, fmt.Errorf("%s: jwks_url: %w", oidcPrefix, err))
				}
			}
		}
	}

	return errors.Join(errs...)
}

func validatePEM(pemData string) error {
	block, _ := pem.Decode([]byte(pemData))
	if block == nil {
		return errors.New("no PEM block found")
	}

	// Try parsing as a public key
	if _, err := x509.ParsePKIXPublicKey(block.Bytes); err != nil {
		// Try parsing as a PKCS1 public key (RSA)
		if _, err2 := x509.ParsePKCS1PublicKey(block.Bytes); err2 != nil {
			return fmt.Errorf("not a valid public key: %w", err)
		}
	}

	return nil
}

func validateHTTPSURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("must use https scheme, got %q", u.Scheme)
	}
	if u.Host == "" {
		return errors.New("host is required")
	}
	return nil
}
