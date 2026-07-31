package deliver

import (
	"bytes"
	"crypto/rsa"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Songmu/go-httpdate"
	"github.com/sirupsen/logrus"
	"github.com/thystra/Activity-Relay/internal/httpsignature"
)

const maxDeliveryResponseBodyBytes int64 = 4096

func appendSignature(request *http.Request, body *[]byte, KeyID string, privateKey *rsa.PrivateKey) error {
	signer, err := httpsignature.NewSigner(KeyID, privateKey)
	if err != nil {
		return err
	}
	return signer.SignPOST(request, *body)
}

type deliveryResponse struct {
	StatusCode    int
	Status        string
	Body          string
	BodyTruncated bool
}

func sendActivity(inboxURL string, KeyID string, body []byte, privateKey *rsa.PrivateKey) error {
	_, err := sendActivityWithResponse(inboxURL, KeyID, body, privateKey)
	return err
}

func sendActivityWithResponse(
	inboxURL string,
	KeyID string,
	body []byte,
	privateKey *rsa.PrivateKey,
) (deliveryResponse, error) {
	result := deliveryResponse{}

	req, err := http.NewRequest(http.MethodPost, inboxURL, bytes.NewReader(body))
	if err != nil {
		return result, fmt.Errorf("create delivery request: %w", err)
	}

	req.Header.Set("Content-Type", "application/activity+json")
	req.Header.Set(
		"User-Agent",
		fmt.Sprintf(
			"%s (golang net/http; Activity-Relay %s; %s)",
			GlobalConfig.ServerServiceName(),
			version,
			GlobalConfig.ServerHostname().Host,
		),
	)
	req.Header.Set("Date", httpdate.Time2Str(time.Now()))

	if err := appendSignature(req, &body, KeyID, privateKey); err != nil {
		return result, fmt.Errorf("sign delivery request: %w", err)
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
		return result, errors.New(inboxURL + ": " + errMsg)
	}
	defer resp.Body.Close()

	result.StatusCode = resp.StatusCode
	result.Status = resp.Status

	responseBody, readErr := io.ReadAll(
		io.LimitReader(resp.Body, maxDeliveryResponseBodyBytes+1),
	)
	result.BodyTruncated = int64(len(responseBody)) > maxDeliveryResponseBodyBytes
	if result.BodyTruncated {
		responseBody = responseBody[:maxDeliveryResponseBodyBytes]
	}
	result.Body = strings.Join(strings.Fields(string(responseBody)), " ")

	logrus.Debug(inboxURL, " ", resp.StatusCode)

	if resp.StatusCode/100 != 2 {
		if readErr != nil {
			return result, fmt.Errorf(
				"%s: %s: unable to read response body: %w",
				inboxURL,
				resp.Status,
				readErr,
			)
		}
		if result.BodyTruncated && result.Body != "" {
			result.Body += " [truncated]"
		}
		if result.Body != "" {
			return result, fmt.Errorf(
				"%s: %s: %s",
				inboxURL,
				resp.Status,
				result.Body,
			)
		}
		return result, errors.New(inboxURL + ": " + resp.Status)
	}

	return result, nil
}

// EOF: deliver/sender.go
