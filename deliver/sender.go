package deliver

import (
	"bytes"
	"crypto/rsa"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/Songmu/go-httpdate"
	"github.com/go-fed/httpsig"
	"github.com/sirupsen/logrus"
)

var hs2019Pattern = regexp.MustCompile(`algorithm="hs2019"`)

func compatibilityForHTTPSignature11(request *http.Request, algorithm httpsig.Algorithm) {
	signature := request.Header.Get("Signature")
	signature = hs2019Pattern.ReplaceAllString(signature, string("algorithm=\""+algorithm+"\""))
	request.Header.Set("Signature", signature)
}

func appendSignature(request *http.Request, body *[]byte, KeyID string, privateKey *rsa.PrivateKey) error {
	request.Header.Set("Host", request.Host)

	signer, _, err := httpsig.NewSigner([]httpsig.Algorithm{httpsig.RSA_SHA256}, httpsig.DigestSha256, []string{httpsig.RequestTarget, "Host", "Date", "Digest", "Content-Type"}, httpsig.Signature, 60*60)
	if err != nil {
		return err
	}
	err = signer.SignRequest(privateKey, KeyID, request, *body)
	if err != nil {
		return err
	}
	compatibilityForHTTPSignature11(request, httpsig.RSA_SHA256) // Compatibility for Misskey <12.111.0
	return nil
}

func sendActivity(inboxURL string, KeyID string, body []byte, privateKey *rsa.PrivateKey) error {
	req, err := http.NewRequest(http.MethodPost, inboxURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create delivery request: %w", err)
	}
	req.Header.Set("Content-Type", "application/activity+json")
	req.Header.Set("User-Agent", fmt.Sprintf("%s (golang net/http; Activity-Relay %s; %s)", GlobalConfig.ServerServiceName(), version, GlobalConfig.ServerHostname().Host))
	req.Header.Set("Date", httpdate.Time2Str(time.Now()))
	if err := appendSignature(req, &body, KeyID, privateKey); err != nil {
		return fmt.Errorf("sign delivery request: %w", err)
	}
	resp, err := HttpClient.Do(req)
	if err != nil {
		var urlErr *url.Error
		errMsg := ""

		if errors.As(err, &urlErr) && urlErr.Timeout() {
			errMsg = "Client.Timeout exceeded while awaiting headers"
		} else if urlErr != nil {
			errMsg = urlErr.Unwrap().Error()
		} else {
			errMsg = err.Error()
		}
		return errors.New(inboxURL + ": " + errMsg)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	logrus.Debug(inboxURL, " ", resp.StatusCode)
	if resp.StatusCode/100 != 2 {
		return errors.New(inboxURL + ": " + resp.Status)
	}

	return nil
}
