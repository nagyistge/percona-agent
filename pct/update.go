/*
   Copyright (c) 2014, Percona LLC and/or its affiliates. All rights reserved.

   This program is free software: you can redistribute it and/or modify
   it under the terms of the GNU Affero General Public License as published by
   the Free Software Foundation, either version 3 of the License, or
   (at your option) any later version.

   This program is distributed in the hope that it will be useful,
   but WITHOUT ANY WARRANTY; without even the implied warranty of
   MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
   GNU Affero General Public License for more details.

   You should have received a copy of the GNU Affero General Public License
   along with this program.  If not, see <http://www.gnu.org/licenses/>
*/

package pct

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

var PublicKey = []byte(`-----BEGIN PUBLIC KEY-----
MIICIjANBgkqhkiG9w0BAQEFAAOCAg8AMIICCgKCAgEA3Ks0r5mrqcxOj95VLyCC
JGkilUyyqIwK9YANtf1qOghQHM4qR1g22c+4iLalzcKuf7fRFWyEOmthUMJEdaPN
k3bHTvqBOVsKigq1Yjydlrk3uIvCBLA37HbHKqM7Uq3vPG25GGxv3wI6NbS1fJOO
VnX0gUYTEiQrbv5OjSBBX/A1mj2RBbSVmHbRSwogFGrxEcz+cn8ulDp+8BOtUHmj
/4FY4IXjeLHihBv/q4TQrO3m0CSig3hmfPJiGpWFdxO5dldb8HN1L+P2EsI6xSxo
5Rk48UbIRBgZjZKQcRCacM0V4Co+HxmtXQkrNIf7jdX6iYXPUD/c8+k7rM1eLgEQ
5jNKQEAiLBdB8ZhDtA4MgFHu8ygwEdYtydo0zLqU0EmtpdmUqnVidudaqVFSiC2Q
KAlH5+r/arnC47cwxa4XletbFzEUcw8sC8ZeqWRSC4EGBJI3fgxoikXsarDQ+uoo
9Hy7jB6XbSGgOJE8Spw3q39DQsB2l7sBliEvi5cl1svRMwrNrTd4A2juAze/GQ+B
tqLJb/FMM1F7LaCl8nLDTf1jPav1/BN5u2y893gX8e48MyUaDtzdiT5HMzMaGkIc
IaY4e/7W5A+S1qCWV0AXTawBgcorRCnUucIjrq6canVZj7BdZpADysRzMib8fS8O
3ca1+bu7FtdcwOTpZusdRfUCAwEAAQ==
-----END PUBLIC KEY-----`)

type Updater struct {
	logger         *Logger
	api            APIConnector
	currentBin     string
	currentVersion string
	// --
	rsaPubKey *rsa.PublicKey
	major     int64
	minor     int64
	patch     int64
}

func NewUpdater(logger *Logger, api APIConnector, pubKey []byte, currentBin, currentVersion string) *Updater {
	pubkeyMarshaled, _ := pem.Decode(pubKey)
	pubKeyParsed, err := x509.ParsePKIXPublicKey(pubkeyMarshaled.Bytes)
	if err != nil {
		panic(err)
	}
	rsaPubKey := pubKeyParsed.(*rsa.PublicKey)

	major, minor, patch := VersionStringToInts(currentVersion)
	u := &Updater{
		logger:         logger,
		api:            api,
		currentBin:     currentBin, // filepath.Abs(os.Args[0])
		currentVersion: currentVersion,
		// --
		rsaPubKey: rsaPubKey,
		major:     major,
		minor:     minor,
		patch:     patch,
	}
	return u
}

func (u *Updater) Check() (string, string, error) {
	url := fmt.Sprintf("%s/latest", u.api.EntryLink("download"), u.major, u.minor)
	v, err := u.download(url)
	if err != nil {
		return "", "", err
	}
	version := string(v)
	major, minor, patch := VersionStringToInts(version)
	switch {
	case major > u.major:
		return "major", version, nil
	case minor > u.minor:
		return "minor", version, nil
	case patch > u.patch:
		return "patch", version, nil
	default:
		return "", "", nil
	}
}

func (u *Updater) Update(version string) error {
	u.logger.Info("Updating to", version)

	// Download and decompress the gzipped bin and its signature.
	url := fmt.Sprintf("%s/%s/percona-agent-%s", u.api.EntryLink("download"), version, version)
	data, err := u.download(url + ".gz")
	if err != nil {
		return err
	}
	sig, err := u.download(url + ".sig")
	if err != nil {
		return err
	}

	// Check the binary's signature.  It's signed by Percona.
	if err = u.checkSignature(data, sig); err != nil {
		return err
	}

	// Write binary to disk as /tmp/percona-agent-<version>.
	newBin := filepath.Join(os.TempDir(), fmt.Sprintf("percona-agent-%s-%s", version, runtime.GOARCH))
	u.logger.Debug("Update:write:" + newBin)
	if err := ioutil.WriteFile(newBin, data, os.FileMode(0755)); err != nil {
		return err
	}

	// Run new binary -version to make sure it runs and returns the version.
	u.logger.Debug("Update:exec")
	out, err := exec.Command(newBin, "-version").Output()
	if err != nil {
		return err
	}
	u.logger.Debug("Update:exec:" + string(out))
	if strings.TrimSpace(string(out)) != "percona-agent "+version {
		return fmt.Errorf("%s -version returns %s, expected %s", newBin, out, version)
	}

	// Overwrite the current, running binary with new bin.
	u.logger.Info("Moving", newBin, "to", u.currentBin)
	if err := os.Rename(newBin, u.currentBin); err != nil {
		return err
	}

	u.logger.Info("Update complete; restart percona-agent")
	return nil
}

func (u *Updater) download(url string) ([]byte, error) {
	u.logger.Debug("download:call:" + url)
	defer u.logger.Debug("download:call")

	u.logger.Info("Downloading", url)

	code, data, err := u.api.Get(u.api.ApiKey(), url)
	u.logger.Debug(fmt.Sprintf("download:code:%d", code))
	if err != nil {
		return nil, fmt.Errorf("GET %s error: %s", err)
	}
	if code != 200 {
		return nil, fmt.Errorf("GET %s returned %d, expected 200", url, code)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("GET %s did not return any data", url)
	}
	return data, nil
}

func (u *Updater) checkSignature(data, sig []byte) error {
	u.logger.Debug("checkSignature:call")
	defer u.logger.Debug("checkSignature:return")
	hash := sha256.New()
	hash.Write(data)
	return rsa.VerifyPKCS1v15(u.rsaPubKey, crypto.SHA256, hash.Sum(nil), sig)
}

func VersionStringToInts(version string) (int64, int64, int64) {
	v := strings.SplitN(version, ".", 3)
	major, _ := strconv.ParseInt(v[0], 10, 8)
	minor, _ := strconv.ParseInt(v[1], 10, 8)
	patch, _ := strconv.ParseInt(v[2], 10, 8)
	return major, minor, patch
}
