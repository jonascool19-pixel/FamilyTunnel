package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

// Root-managed, optional ownership proofs; never accepted from a public API.
// Serve only Google's exact verification path on an already enabled web route.
type siteVerification struct {
	Domain string `json:"domain"`
	File   string `json:"file"`
}

var googleVerificationFile = regexp.MustCompile(`^google[a-f0-9]{16}\.html$`)

func loadSiteVerifications(dir string) ([]siteVerification, error) {
	f, err := os.Open(filepath.Join(dir, "site-verification.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("site verification configuration unavailable")
	}
	defer f.Close()
	if info, statErr := f.Stat(); statErr != nil || info.Size() > 16384 {
		return nil, fmt.Errorf("site verification configuration exceeds size limit")
	}
	d := json.NewDecoder(io.LimitReader(f, 16385))
	d.DisallowUnknownFields()
	var proofs []siteVerification
	if err = d.Decode(&proofs); err != nil || len(proofs) > 32 {
		return nil, fmt.Errorf("invalid site verification configuration")
	}
	if err = d.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("invalid site verification configuration")
	}
	for _, proof := range proofs {
		if len(proof.Domain) > 253 || !domainRE.MatchString(proof.Domain) || !googleVerificationFile.MatchString(proof.File) {
			return nil, fmt.Errorf("invalid site verification domain or filename")
		}
	}
	return proofs, nil
}
