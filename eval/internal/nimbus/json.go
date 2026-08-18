// Package nimbus implements the hermetic Milestone 2 calibration fixtures.
package nimbus

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const maxManifestBytes = 256 << 10

func decodeStrict(data []byte, dst any) error {
	if len(data) > maxManifestBytes {
		return fmt.Errorf("manifest exceeds %d bytes", maxManifestBytes)
	}
	if err := rejectDuplicateKeys(data); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON value")
	}
	return nil
}

func rejectDuplicateKeys(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	var walk func() error
	walk = func() error {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for dec.More() {
				keyToken, err := dec.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok {
					return errors.New("object key is not a string")
				}
				if seen[key] {
					return fmt.Errorf("duplicate key %q", key)
				}
				seen[key] = true
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = dec.Token()
			return err
		case '[':
			for dec.More() {
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = dec.Token()
			return err
		default:
			return fmt.Errorf("unexpected delimiter %q", delim)
		}
	}
	if err := walk(); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("trailing JSON value")
	}
	return nil
}

func validID(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	for _, r := range s {
		switch {
		case r == '-', r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return !strings.Contains(s, "--")
}
