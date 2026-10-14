package generation

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"strings"
)

const videoDeliveryIdleTimeout = 10 * time.Second

const beefAPIVideoDeliveryHeader = "X-Beeftv-Video-Delivery"

// Opt in only at the trusted gateway's video status endpoint. Older clients
// keep their existing delivery route until they support same-task backup.
func applyBeefAPIVideoDeliveryHeader(ctx context.Context, config Config, req *http.Request) {
	if !IsBeefAPIVideoConfig(ctx, config) || req.Method != http.MethodGet || !SameProviderOrigin(config.BaseURL, req.URL.String()) {
		return
	}
	collectionURL, err := url.Parse(APIURL(config.BaseURL, "/videos"))
	if err != nil {
		return
	}
	prefix := collectionURL.Path + "/"
	if !strings.HasPrefix(req.URL.Path, prefix) {
		return
	}
	id := strings.TrimPrefix(req.URL.Path, prefix)
	if id != "" && !strings.Contains(id, "/") {
		req.Header.Set(beefAPIVideoDeliveryHeader, "direct")
	}
}

type videoDeliveryRequestContext struct{}

// DownloadVideoResult retrieves an existing task's media, including renewable
// backup links. It never submits generation and shares the worker's retry loop.
func DownloadVideoResult(ctx context.Context, config Config, rawURL, taskID string) ([]byte, string, error) {
	policy := videoPollPolicyForConfig(ctx, config, DefaultVideoPollPolicy())
	data, mimeType, err := RunVideoDownload(ctx, taskID, policy, videoResultDownloader(config, rawURL, taskID))
	return data, NormalizedMediaMIMEType(mimeType, data), err
}

// videoBackupLinkExpiredError distinguishes a rejected external signed URL
// from a rejected credential at the authenticated gateway.
type videoBackupLinkExpiredError struct{ cause error }

func (e videoBackupLinkExpiredError) Error() string { return e.cause.Error() }
func (e videoBackupLinkExpiredError) Unwrap() error { return e.cause }

// A download keeps the original provider task. Once its primary route fails,
// each retry requests a fresh backup link rather than reusing the failed URL.
func videoResultDownloader(config Config, rawURL, taskID string) func(context.Context) ([]byte, string, error) {
	backup := false
	return func(ctx context.Context) ([]byte, string, error) {
		ctx = WithRequestKind(ctx, "download")
		if !IsBeefAPIVideoConfig(ctx, config) || strings.TrimSpace(taskID) == "" {
			return GetProviderExternalBinary(ctx, config, rawURL)
		}
		if !backup {
			data, mimeType, err := getVideoBinaryWithIdleTimeout(ctx, config, rawURL, videoDeliveryIdleTimeout)
			if err == nil {
				return data, mimeType, nil
			}
			if ctx.Err() != nil {
				return nil, "", ctx.Err()
			}
			retry, _ := RetryableVideoPollError(ctx, err)
			var httpErr HTTPError
			// An external media URL may expire while the gateway credential is
			// valid. Authentication rejection at the gateway must still stop.
			expired := !SameProviderOrigin(config.BaseURL, ProviderDownloadURL(config.BaseURL, rawURL)) && errors.As(err, &httpErr) && (httpErr.StatusCode == http.StatusUnauthorized || httpErr.StatusCode == http.StatusForbidden)
			if !retry && !expired {
				return nil, "", err
			}
			backup = true
		}
		backupURL := APIURL(config.BaseURL, "/videos/"+url.PathEscape(taskID)+"/content") + "?delivery=backup"
		return getVideoBinaryWithIdleTimeout(ctx, config, backupURL, videoDeliveryIdleTimeout)
	}
}

func getVideoBinaryWithIdleTimeout(ctx context.Context, config Config, rawURL string, idle time.Duration) ([]byte, string, error) {
	return GetProviderExternalBinary(context.WithValue(ctx, videoDeliveryRequestContext{}, idle), config, rawURL)
}
