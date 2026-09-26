package focus

import (
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/pkg/connector"
	"github.com/kairn-io/kairn/pkg/model"
)

const export = "BillingPeriodStart,ChargePeriodStart,ChargePeriodEnd,BilledCost,EffectiveCost,BillingCurrency,ResourceId,ResourceName,ServiceName,ServiceCategory,SkuId,ChargeCategory,ConsumedQuantity,ConsumedUnit,RegionId,Tags\n" +
	`2026-09-01T00:00:00Z,2026-09-10T00:00:00Z,2026-09-11T00:00:00Z,2.40,2.10,EUR,arn:aws:ec2:eu-west-3:123:instance/i-0abc,,Amazon EC2,Compute,m7g.large,Usage,24,Hours,eu-west-3,"{""team"":""data"",""env"":""prod""}"` + "\n" +
	`2026-09-01T00:00:00Z,2026-09-10T00:00:00Z,2026-09-11T00:00:00Z,0.80,0.80,EUR,arn:aws:s3:::datalake,datalake,Amazon S3,Storage,TimedStorage,Usage,100,GB-Mo,eu-west-3,{}` + "\n" +
	`2026-09-01T00:00:00Z,2026-09-10T00:00:00Z,2026-09-11T00:00:00Z,-1.00,-1.00,EUR,,,AWS Credits,,credit,Credit,,,,` + "\n" +
	`2026-09-01T00:00:00Z,2026-08-20T00:00:00Z,2026-08-21T00:00:00Z,9.99,9.99,EUR,arn:aws:ec2:eu-west-3:123:instance/i-old,,Amazon EC2,Compute,m7g.large,Usage,24,Hours,eu-west-3,` + "\n"

func server(t *testing.T) *httptest.Server {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	_, _ = gz.Write([]byte(export))
	_ = gz.Close()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/export.csv.gz" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write(buf.Bytes())
	}))
	t.Cleanup(srv.Close)
	return srv
}

func conn(t *testing.T, srv *httptest.Server) *Conn {
	t.Helper()
	c, err := New(connector.Config{Settings: map[string]string{"source": srv.URL + "/export.csv.gz", "provider": "aws"}})
	if err != nil {
		t.Fatal(err)
	}
	cc := c.(*Conn)
	cc.http = srv.Client()
	cc.now = func() time.Time { return time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC) }
	return cc
}

func TestFocusImport(t *testing.T) {
	c := conn(t, server(t))
	if err := c.Validate(context.Background(), connector.Config{}); err != nil {
		t.Fatal(err)
	}
	ctx, sink := connector.WithErrorSink(context.Background())
	ch, _ := c.SyncInventory(ctx, time.Time{})
	res := connector.Collect(ch)
	if err := sink.Err(); err != nil {
		t.Fatal(err)
	}
	// La ressource facturée il y a plus de 30 jours n'est plus dans l'inventaire.
	if len(res) != 2 || res[0].Type != model.TypeInstance || res[0].Name != "i-0abc" || res[0].Labels["team"] != "data" || res[1].Type != model.TypeBucket {
		t.Fatalf("inventory: %+v", res)
	}
	bctx, bsink := connector.WithErrorSink(context.Background())
	bch, _ := c.SyncBilling(bctx, connector.Period{From: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)})
	lines := connector.Collect(bch)
	if err := bsink.Err(); err != nil {
		t.Fatal(err)
	}
	total := decimal.Zero
	for _, l := range lines {
		total = total.Add(l.Amount)
	}
	// EffectiveCost par défaut (engagements amortis) ; les crédits sont négatifs.
	if len(lines) != 3 || !total.Equal(decimal.RequireFromString("1.90")) || lines[2].CostType != model.CostCredit {
		t.Fatalf("billing: %+v total %s", lines, total)
	}
	if _, err := New(connector.Config{Settings: map[string]string{"source": "ftp://x/y"}}); err == nil {
		t.Fatal("unsupported scheme accepted")
	}
	if _, err := New(connector.Config{Settings: map[string]string{"source": "s3://bucket/prefix"}}); err == nil {
		t.Fatal("s3 without credentials accepted")
	}
}

func TestNotAFocusFile(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("a,b\n1,2\n")) }))
	defer srv.Close()
	c, _ := New(connector.Config{Settings: map[string]string{"source": srv.URL + "/x.csv"}})
	cc := c.(*Conn)
	cc.http = srv.Client()
	ctx, sink := connector.WithErrorSink(context.Background())
	ch, _ := cc.SyncBilling(ctx, connector.Period{From: time.Time{}, To: time.Now()})
	connector.Collect(ch)
	if sink.Err() == nil {
		t.Fatal("non-FOCUS file must be reported")
	}
}
