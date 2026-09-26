package ovh

import (
	"context"
	"crypto/sha1" //nolint:gosec // imposé par le schéma de signature de l'API OVHcloud
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kairn-io/kairn/connectors/internal/rest"
)

// Points d'accès de l'API OVHcloud.
var endpoints = map[string]string{
	"ovh-eu": "https://eu.api.ovh.com/1.0",
	"ovh-ca": "https://ca.api.ovh.com/1.0",
	"ovh-us": "https://api.us.ovhcloud.com/1.0",
}

// client signe les requêtes selon le schéma de l'API OVHcloud :
// "$1$" + sha1(AS+"+"+CK+"+"+méthode+"+"+URL+"+"+corps+"+"+horodatage).
type client struct {
	base          string
	appKey        string
	appSecret     string
	consumerKey   string
	http          *http.Client
	now           func() time.Time
	mu            sync.Mutex
	delta         time.Duration // écart d'horloge avec l'API
	deltaResolved bool
}

func (c *client) timeDelta(ctx context.Context) (time.Duration, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.deltaResolved {
		return c.delta, nil
	}
	var server int64
	if err := (&rest.Client{HTTP: c.http}).Get(ctx, c.base+"/auth/time", &server); err != nil {
		return 0, fmt.Errorf("ovh time: %w", err)
	}
	c.delta = time.Unix(server, 0).Sub(c.now())
	c.deltaResolved = true
	return c.delta, nil
}

func sign(appSecret, consumerKey, method, url, body string, ts int64) string {
	h := sha1.Sum([]byte(strings.Join([]string{appSecret, consumerKey, method, url, body, strconv.FormatInt(ts, 10)}, "+"))) //nolint:gosec
	return "$1$" + hex.EncodeToString(h[:])
}

// get lit une ressource JSON signée.
func (c *client) get(ctx context.Context, path string, out any) error {
	delta, err := c.timeDelta(ctx)
	if err != nil {
		return err
	}
	u := c.base + path
	ts := c.now().Add(delta).Unix()
	rc := &rest.Client{HTTP: c.http, Auth: func(r *http.Request) {
		r.Header.Set("X-Ovh-Application", c.appKey)
		r.Header.Set("X-Ovh-Consumer", c.consumerKey)
		r.Header.Set("X-Ovh-Timestamp", strconv.FormatInt(ts, 10))
		r.Header.Set("X-Ovh-Signature", sign(c.appSecret, c.consumerKey, http.MethodGet, u, "", ts))
	}}
	return rc.Get(ctx, u, out)
}
