// Package apikey loads backend-only credentials. Never send these to browsers.
package apikey

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func Load(path string, create bool) (string, error) {
	if value := strings.TrimSpace(os.Getenv("GAME_API_TOKEN")); value != "" {
		return value, nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) && create {
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return "", err
		}
		secret := make([]byte, 32)
		if _, err = rand.Read(secret); err != nil {
			return "", err
		}
		data = []byte(hex.EncodeToString(secret))
		file, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if os.IsExist(e) {
			return Load(path, false)
		}
		if e != nil {
			return "", e
		}
		_, e = file.Write(data)
		if e == nil {
			e = file.Sync()
		}
		closeErr := file.Close()
		if e != nil {
			return "", e
		}
		if closeErr != nil {
			return "", closeErr
		}
	} else if err != nil {
		return "", fmt.Errorf("load GAME_API_TOKEN_FILE: %w", err)
	}
	key := strings.TrimSpace(string(data))
	if key == "" {
		return "", fmt.Errorf("empty API key file")
	}
	return key, nil
}
