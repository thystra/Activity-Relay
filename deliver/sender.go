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

func directDeliveryProfile() httpsignature.Profile {
	if OutboundRequestSigner != nil {
		profile := OutboundRequestSigner.Profile()
		if profile != httpsignature.ProfileDual && profile != "" {
			return profile
		}
	}
	if GlobalConfig != nil {
		profile := GlobalConfig.OutboundSignatureProfile()
		if profile != httpsignature.ProfileDual && profile != "" {
			return profile
		}
	}
	return httpsignature.ProfileLegacy
}

func appendSignatureWithProfile(
	request *http.Request,
	body *[]byte,
	KeyID string,
	privateKey *rsa.PrivateKey,
	profile httpsignature.Profile,
) error {
	if OutboundRequestSigner != nil {
		return OutboundRequestSigner.SignPOSTWithProfile(
			request,
			*body,
			profile,
		)
	}
	signer, err := httpsignature.NewConfiguredSigner(
		KeyID,
		privateKey,
		profile,
	)
	if err != nil {
		return err
	}
	return signer.SignPOSTWithProfile(request, *body, profile)
}

func appendSignature(
	request *http.Request,
	body *[]byte,
	KeyID string,
	privateKey *rsa.PrivateKey,
) error {
	return appendSignatureWithProfile(
		request,
		body,
		KeyID,
		privateKey,
		directDeliveryProfile(),
	)
}

type deliveryResponse struct {
	StatusCode    int
	Status        string
	Header        http.Header
	Body          string
	BodyTruncated bool
}

func sendActivity(
	inboxURL string,
	KeyID string,
	body []byte,
	privateKey *rsa.PrivateKey,
) error {
	_, err := sendActivityWithResponse(
		inboxURL,
		KeyID,
		body,
		privateKey,
	)
	return err
}

func sendActivityWithResponse(
	inboxURL string,
	KeyID string,
	body []byte,
	privateKey *rsa.PrivateKey,
) (deliveryResponse, error) {
	return sendActivityWithResponseProfile(
		inboxURL,
		KeyID,
		body,
		privateKey,
		directDeliveryProfile(),
	)
}

func sendActivityWithResponseProfile(
	inboxURL string,
	KeyID string,
	body []byte,
	privateKey *rsa.PrivateKey,
	profile httpsignature.Profile,
) (deliveryResponse, error) {
	result := deliveryResponse{}
	req, err := http.NewRequest(
		http.MethodPost,
		inboxURL,
		bytes.NewReader(body),
	)
	if err != nil {
		return result, fmt.Errorf(
			"create delivery request: %w",
			err,
		)
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
	if err := appendSignatureWithProfile(
		req,
		&body,
		KeyID,
		privateKey,
		profile,
	); err != nil {
		return result, fmt.Errorf(
			"sign delivery request: %w",
			err,
		)
	}
	resp, err := HttpClient.Do(req)
	if err != nil {
		var urlErr *url.Error
		errMsg := ""
		if errors.As(err, &urlErr) && urlErr.Timeout() {
			errMsg =
				"Client.Timeout exceeded while awaiting headers"
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
	result.Header = resp.Header.Clone()
	responseBody, readErr := io.ReadAll(
		io.LimitReader(
			resp.Body,
			maxDeliveryResponseBodyBytes+1,
		),
	)
	result.BodyTruncated =
		int64(len(responseBody)) > maxDeliveryResponseBodyBytes
	if result.BodyTruncated {
		responseBody = responseBody[:maxDeliveryResponseBodyBytes]
	}
	result.Body = strings.Join(
		strings.Fields(string(responseBody)),
		" ",
	)

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
