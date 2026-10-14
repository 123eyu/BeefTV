package generation

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"infinite-canvas/backend/internal/protocol"
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
type videoProtocolAuthContext struct{}
type videoCredentialQueryContext struct{}
type videoCredentialContext struct{}

type videoProviderCredentials struct {
	Origin string
	Values []string
}

type videoResultURLResolver func(context.Context) (string, error)
type VideoResultSource struct {
	URL              string
	Read             func(context.Context) ([]byte, string, error)
	CredentialValues []string
}
type videoResultResolver func(context.Context) (VideoResultSource, error)

func withVideoProtocolAuth(ctx context.Context, auth protocol.ManifestAuth) context.Context {
	return context.WithValue(ctx, videoProtocolAuthContext{}, auth)
}

func withVideoCredentials(ctx context.Context, config Config, extra []string) context.Context {
	values := append([]string{config.APIKey, config.SecretKey}, extra...)
	if auth, ok := ctx.Value(videoProtocolAuthContext{}).(protocol.ManifestAuth); ok {
		credential := ProtocolCredentialField(config, auth.Field)
		values = append(values, credential, auth.Prefix+credential, ProtocolCredentialField(config, auth.SecretField))
	}
	for _, header := range config.Headers {
		values = append(values, header.Value)
		if strings.EqualFold(header.Name, "Cookie") {
			req := &http.Request{Header: http.Header{"Cookie": []string{header.Value}}}
			for _, cookie := range req.Cookies() {
				values = append(values, cookie.Value)
			}
		}
		if strings.EqualFold(header.Name, "Authorization") && strings.HasPrefix(strings.ToLower(header.Value), "bearer ") {
			values = append(values, strings.TrimSpace(header.Value[len("Bearer "):]))
		}
	}
	return context.WithValue(ctx, videoCredentialContext{}, videoProviderCredentials{Origin: config.BaseURL, Values: values})
}

func stripVideoCredentialQuery(target *url.URL, values []string) {
	query := target.Query()
	changed := false
	for name, items := range query {
		keep := make([]string, 0, len(items))
		for _, item := range items {
			secret := false
			for _, value := range values {
				if value != "" && item == value {
					secret = true
					break
				}
			}
			if secret {
				changed = true
			} else {
				keep = append(keep, item)
			}
		}
		if len(keep) == 0 {
			delete(query, name)
		} else {
			query[name] = keep
		}
	}
	if changed {
		target.RawQuery = query.Encode()
	}
}

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
func videoResultDownloader(config Config, rawURL, taskID string, renew ...videoResultURLResolver) func(context.Context) ([]byte, string, error) {
	var resolver videoResultResolver
	if len(renew) > 0 {
		resolver = func(ctx context.Context) (VideoResultSource, error) {
			freshURL, err := renew[0](ctx)
			return videoURLSource(config, freshURL), err
		}
	}
	return VideoResultDownloader(config, taskID, videoURLSource(config, rawURL), resolver)
}

func videoURLSource(config Config, rawURL string) VideoResultSource {
	return VideoResultSource{URL: rawURL, Read: func(ctx context.Context) ([]byte, string, error) {
		if strings.TrimSpace(rawURL) == "" {
			return nil, "", errors.New("原视频任务没有返回可取回的结果")
		}
		if strings.HasPrefix(strings.TrimSpace(rawURL), "data:") {
			mimeType, data, err := DecodeProviderDataURL(rawURL)
			return data, mimeType, err
		}
		return GetProviderExternalBinary(ctx, config, rawURL)
	}}
}

// The existing download loop owns retry/deadline/progress. This closure only
// changes its source: BeefAPI backup, or the same third-party task's fresh URL.
func VideoResultDownloader(config Config, taskID string, primary VideoResultSource, renew func(context.Context) (VideoResultSource, error)) func(context.Context) ([]byte, string, error) {
	refresh := false
	readFresh := func(ctx context.Context) ([]byte, string, error) {
		if IsBeefAPIVideoConfig(ctx, config) {
			backupURL := APIURL(config.BaseURL, "/videos/"+url.PathEscape(taskID)+"/content") + "?delivery=backup"
			return getVideoBinaryWithIdleTimeout(ctx, config, backupURL, videoDeliveryIdleTimeout)
		}
		fresh, err := renew(withDownloadObserver(ctx, nil))
		if err != nil {
			return nil, "", err
		}
		ctx = withVideoCredentials(ctx, config, append(append([]string{}, primary.CredentialValues...), fresh.CredentialValues...))
		data, mimeType, err := fresh.Read(context.WithValue(ctx, videoDeliveryRequestContext{}, videoDeliveryIdleTimeout))
		if videoExternalLinkExpired(config, fresh.URL, err) {
			err = videoBackupLinkExpiredError{cause: err}
		}
		return data, mimeType, err
	}
	return func(ctx context.Context) ([]byte, string, error) {
		ctx = WithRequestKind(ctx, "download")
		ctx = withVideoCredentials(ctx, config, primary.CredentialValues)
		if refresh {
			return readFresh(ctx)
		}
		data, mimeType, err := primary.Read(context.WithValue(ctx, videoDeliveryRequestContext{}, videoDeliveryIdleTimeout))
		if err == nil {
			return data, mimeType, nil
		}
		if ctx.Err() != nil {
			return nil, "", ctx.Err()
		}
		retry, _ := RetryableVideoPollError(ctx, err)
		if !retry && !videoExternalLinkExpired(config, primary.URL, err) {
			return nil, "", err
		}
		if strings.TrimSpace(taskID) == "" || (!IsBeefAPIVideoConfig(ctx, config) && renew == nil) {
			return nil, "", err
		}
		refresh = true
		return readFresh(ctx)
	}
}

func videoExternalLinkExpired(config Config, rawURL string, err error) bool {
	var httpErr HTTPError
	return !SameProviderOrigin(config.BaseURL, ProviderDownloadURL(config.BaseURL, rawURL)) && errors.As(err, &httpErr) && (httpErr.StatusCode == http.StatusUnauthorized || httpErr.StatusCode == http.StatusForbidden)
}

func videoResultURL(status, rawURL string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "succeeded", "success", "done":
		if strings.TrimSpace(rawURL) == "" {
			return "", errors.New("视频任务成功但没有返回可取回的结果")
		}
		return rawURL, nil
	case "failed", "cancelled", "expired":
		return "", errors.New("原视频任务未成功，无法取回结果")
	default:
		return "", HTTPError{StatusCode: http.StatusServiceUnavailable, Body: `{"error":{"code":"video_delivery_pending"}}`}
	}
}

func getVideoBinaryWithIdleTimeout(ctx context.Context, config Config, rawURL string, idle time.Duration) ([]byte, string, error) {
	return GetProviderExternalBinary(context.WithValue(ctx, videoDeliveryRequestContext{}, idle), config, rawURL)
}
