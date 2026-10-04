package directoryscheduler

import (
	"context"
	"github.com/thystra/Activity-Relay/internal/directoryclient"
)

type negotiatedDirectoryClient interface {
	RegisterNegotiated(context.Context) (directoryclient.Response, error)
	HeartbeatWithRegisterReconciliationNegotiated(context.Context) (directoryclient.Response, error)
}

type profileDigestClient interface{ ProfileDigest() string }

func schedulerRegister(ctx context.Context, client Client) (directoryclient.Response, error) {
	if negotiated, ok := client.(negotiatedDirectoryClient); ok {
		return negotiated.RegisterNegotiated(ctx)
	}
	return client.Register(ctx)
}

func schedulerHeartbeat(ctx context.Context, client Client) (directoryclient.Response, error) {
	if negotiated, ok := client.(negotiatedDirectoryClient); ok {
		return negotiated.HeartbeatWithRegisterReconciliationNegotiated(ctx)
	}
	return client.HeartbeatWithRegisterReconciliation(ctx)
}

func clientProfileDigest(client Client) (string, bool) {
	profiled, ok := client.(profileDigestClient)
	if !ok {
		return "", false
	}
	digest := profiled.ProfileDigest()
	return digest, len(digest) == 64
}
