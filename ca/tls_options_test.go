package ca

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"net/http"
	"reflect"
	"sort"
	"testing"
)

func TestTLSOptionCtx_apply(t *testing.T) {
	fail := func() TLSOption {
		return func(ctx *TLSOptionCtx) error {
			return fmt.Errorf("an error")
		}
	}

	type fields struct {
		Config *tls.Config
	}
	type args struct {
		options []TLSOption
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		{"ok", fields{&tls.Config{}}, args{[]TLSOption{RequireAndVerifyClientCert()}}, false},
		{"ok", fields{&tls.Config{}}, args{[]TLSOption{VerifyClientCertIfGiven()}}, false},
		{"fail", fields{&tls.Config{}}, args{[]TLSOption{VerifyClientCertIfGiven(), fail()}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &TLSOptionCtx{
				Config: tt.fields.Config,
			}
			if err := ctx.apply(tt.args.options); (err != nil) != tt.wantErr {
				t.Errorf("TLSOptionCtx.apply() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRequireAndVerifyClientCert(t *testing.T) {
	tests := []struct {
		name string
		want *tls.Config
	}{
		{"ok", &tls.Config{ClientAuth: tls.RequireAndVerifyClientCert}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &TLSOptionCtx{
				Config: &tls.Config{},
			}
			if err := RequireAndVerifyClientCert()(ctx); err != nil {
				t.Errorf("RequireAndVerifyClientCert() error = %v", err)
				return
			}
			if !reflect.DeepEqual(ctx.Config, tt.want) {
				t.Errorf("RequireAndVerifyClientCert() = %v, want %v", ctx.Config, tt.want)
			}
		})
	}
}

func TestVerifyClientCertIfGiven(t *testing.T) {
	tests := []struct {
		name string
		want *tls.Config
	}{
		{"ok", &tls.Config{ClientAuth: tls.VerifyClientCertIfGiven}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &TLSOptionCtx{
				Config: &tls.Config{},
			}
			if err := VerifyClientCertIfGiven()(ctx); err != nil {
				t.Errorf("VerifyClientCertIfGiven() error = %v", err)
				return
			}
			if !reflect.DeepEqual(ctx.Config, tt.want) {
				t.Errorf("VerifyClientCertIfGiven() = %v, want %v", ctx.Config, tt.want)
			}
		})
	}
}

func TestAddRootCA(t *testing.T) {
	cert := parseCertificate(rootPEM)
	pool := x509.NewCertPool()
	pool.AddCert(cert)

	type args struct {
		cert *x509.Certificate
	}
	tests := []struct {
		name string
		args args
		want *tls.Config
	}{
		{"ok", args{cert}, &tls.Config{RootCAs: pool}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &TLSOptionCtx{
				Config: &tls.Config{},
			}
			if err := AddRootCA(tt.args.cert)(ctx); err != nil {
				t.Errorf("AddRootCA() error = %v", err)
				return
			}
			if !reflect.DeepEqual(ctx.Config, tt.want) {
				t.Errorf("AddRootCA() = %v, want %v", ctx.Config, tt.want)
			}
		})
	}
}

func TestAddClientCA(t *testing.T) {
	cert := parseCertificate(rootPEM)
	pool := x509.NewCertPool()
	pool.AddCert(cert)

	type args struct {
		cert *x509.Certificate
	}
	tests := []struct {
		name string
		args args
		want *tls.Config
	}{
		{"ok", args{cert}, &tls.Config{ClientCAs: pool}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &TLSOptionCtx{
				Config: &tls.Config{},
			}
			if err := AddClientCA(tt.args.cert)(ctx); err != nil {
				t.Errorf("AddClientCA() error = %v", err)
				return
			}
			if !reflect.DeepEqual(ctx.Config, tt.want) {
				t.Errorf("AddClientCA() = %v, want %v", ctx.Config, tt.want)
			}
		})
	}
}

func TestAddRootsToRootCAs(t *testing.T) {
	ca := startCATestServer()
	defer ca.Close()

	client, sr, pk := signDuration(ca, "127.0.0.1", 0)
	tr, err := getTLSOptionsTransport(sr, pk)
	if err != nil {
		t.Fatal(err)
	}

	root, err := ioutil.ReadFile("testdata/secrets/root_ca.crt")
	if err != nil {
		t.Fatal(err)
	}

	cert := parseCertificate(string(root))
	pool := x509.NewCertPool()
	pool.AddCert(cert)

	tests := []struct {
		name    string
		tr      http.RoundTripper
		want    *tls.Config
		wantErr bool
	}{
		{"ok", tr, &tls.Config{RootCAs: pool}, false},
		{"fail", http.DefaultTransport, &tls.Config{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &TLSOptionCtx{
				Client:    client,
				Config:    &tls.Config{},
				Transport: tt.tr,
			}
			if err := AddRootsToRootCAs()(ctx); (err != nil) != tt.wantErr {
				t.Errorf("AddRootsToRootCAs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(ctx.Config, tt.want) {
				t.Errorf("AddRootsToRootCAs() = %v, want %v", ctx.Config, tt.want)
			}
		})
	}
}

func TestAddRootsToClientCAs(t *testing.T) {
	ca := startCATestServer()
	defer ca.Close()

	client, sr, pk := signDuration(ca, "127.0.0.1", 0)
	tr, err := getTLSOptionsTransport(sr, pk)
	if err != nil {
		t.Fatal(err)
	}

	root, err := ioutil.ReadFile("testdata/secrets/root_ca.crt")
	if err != nil {
		t.Fatal(err)
	}

	cert := parseCertificate(string(root))
	pool := x509.NewCertPool()
	pool.AddCert(cert)

	tests := []struct {
		name    string
		tr      http.RoundTripper
		want    *tls.Config
		wantErr bool
	}{
		{"ok", tr, &tls.Config{ClientCAs: pool}, false},
		{"fail", http.DefaultTransport, &tls.Config{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &TLSOptionCtx{
				Client:    client,
				Config:    &tls.Config{},
				Transport: tt.tr,
			}
			if err := AddRootsToClientCAs()(ctx); (err != nil) != tt.wantErr {
				t.Errorf("AddRootsToClientCAs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(ctx.Config, tt.want) {
				t.Errorf("AddRootsToClientCAs() = %v, want %v", ctx.Config, tt.want)
			}
		})
	}
}

func TestAddFederationToRootCAs(t *testing.T) {
	ca := startCATestServer()
	defer ca.Close()

	client, sr, pk := signDuration(ca, "127.0.0.1", 0)
	tr, err := getTLSOptionsTransport(sr, pk)
	if err != nil {
		t.Fatal(err)
	}

	root, err := ioutil.ReadFile("testdata/secrets/root_ca.crt")
	if err != nil {
		t.Fatal(err)
	}

	federated, err := ioutil.ReadFile("testdata/secrets/federated_ca.crt")
	if err != nil {
		t.Fatal(err)
	}

	crt1 := parseCertificate(string(root))
	crt2 := parseCertificate(string(federated))
	pool := x509.NewCertPool()
	pool.AddCert(crt1)
	pool.AddCert(crt2)

	tests := []struct {
		name    string
		tr      http.RoundTripper
		want    *tls.Config
		wantErr bool
	}{
		{"ok", tr, &tls.Config{RootCAs: pool}, false},
		{"fail", http.DefaultTransport, &tls.Config{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &TLSOptionCtx{
				Client:    client,
				Config:    &tls.Config{},
				Transport: tt.tr,
			}
			if err := AddFederationToRootCAs()(ctx); (err != nil) != tt.wantErr {
				t.Errorf("AddFederationToRootCAs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(ctx.Config, tt.want) {
				// Federated roots are randomly sorted
				if !equalPools(ctx.Config.RootCAs, tt.want.RootCAs) || ctx.Config.ClientCAs != nil {
					t.Errorf("AddFederationToRootCAs() = %v, want %v", ctx.Config, tt.want)
				}
			}
		})
	}
}

func TestAddFederationToClientCAs(t *testing.T) {
	ca := startCATestServer()
	defer ca.Close()

	client, sr, pk := signDuration(ca, "127.0.0.1", 0)
	tr, err := getTLSOptionsTransport(sr, pk)
	if err != nil {
		t.Fatal(err)
	}

	root, err := ioutil.ReadFile("testdata/secrets/root_ca.crt")
	if err != nil {
		t.Fatal(err)
	}

	federated, err := ioutil.ReadFile("testdata/secrets/federated_ca.crt")
	if err != nil {
		t.Fatal(err)
	}

	crt1 := parseCertificate(string(root))
	crt2 := parseCertificate(string(federated))
	pool := x509.NewCertPool()
	pool.AddCert(crt1)
	pool.AddCert(crt2)

	tests := []struct {
		name    string
		tr      http.RoundTripper
		want    *tls.Config
		wantErr bool
	}{
		{"ok", tr, &tls.Config{ClientCAs: pool}, false},
		{"fail", http.DefaultTransport, &tls.Config{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &TLSOptionCtx{
				Client:    client,
				Config:    &tls.Config{},
				Transport: tt.tr,
			}
			if err := AddFederationToClientCAs()(ctx); (err != nil) != tt.wantErr {
				t.Errorf("AddFederationToClientCAs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(ctx.Config, tt.want) {
				// Federated roots are randomly sorted
				if !equalPools(ctx.Config.ClientCAs, tt.want.ClientCAs) || ctx.Config.RootCAs != nil {
					t.Errorf("AddFederationToClientCAs() = %v, want %v", ctx.Config, tt.want)
				}
			}
		})
	}
}

func TestAddRootsToCAs(t *testing.T) {
	ca := startCATestServer()
	defer ca.Close()

	client, sr, pk := signDuration(ca, "127.0.0.1", 0)
	tr, err := getTLSOptionsTransport(sr, pk)
	if err != nil {
		t.Fatal(err)
	}

	root, err := ioutil.ReadFile("testdata/secrets/root_ca.crt")
	if err != nil {
		t.Fatal(err)
	}

	cert := parseCertificate(string(root))
	pool := x509.NewCertPool()
	pool.AddCert(cert)

	tests := []struct {
		name    string
		tr      http.RoundTripper
		want    *tls.Config
		wantErr bool
	}{
		{"ok", tr, &tls.Config{ClientCAs: pool, RootCAs: pool}, false},
		{"fail", http.DefaultTransport, &tls.Config{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &TLSOptionCtx{
				Client:    client,
				Config:    &tls.Config{},
				Transport: tt.tr,
			}
			if err := AddRootsToCAs()(ctx); (err != nil) != tt.wantErr {
				t.Errorf("AddRootsToCAs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(ctx.Config, tt.want) {
				t.Errorf("AddRootsToCAs() = %v, want %v", ctx.Config, tt.want)
			}
		})
	}
}

func TestAddFederationToCAs(t *testing.T) {
	ca := startCATestServer()
	defer ca.Close()

	client, sr, pk := signDuration(ca, "127.0.0.1", 0)
	tr, err := getTLSOptionsTransport(sr, pk)
	if err != nil {
		t.Fatal(err)
	}

	root, err := ioutil.ReadFile("testdata/secrets/root_ca.crt")
	if err != nil {
		t.Fatal(err)
	}

	federated, err := ioutil.ReadFile("testdata/secrets/federated_ca.crt")
	if err != nil {
		t.Fatal(err)
	}

	crt1 := parseCertificate(string(root))
	crt2 := parseCertificate(string(federated))
	pool := x509.NewCertPool()
	pool.AddCert(crt1)
	pool.AddCert(crt2)

	tests := []struct {
		name    string
		tr      http.RoundTripper
		want    *tls.Config
		wantErr bool
	}{
		{"ok", tr, &tls.Config{ClientCAs: pool, RootCAs: pool}, false},
		{"fail", http.DefaultTransport, &tls.Config{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &TLSOptionCtx{
				Client:    client,
				Config:    &tls.Config{},
				Transport: tt.tr,
			}
			if err := AddFederationToCAs()(ctx); (err != nil) != tt.wantErr {
				t.Errorf("AddFederationToCAs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(ctx.Config, tt.want) {
				// Federated roots are randomly sorted
				if !equalPools(ctx.Config.ClientCAs, tt.want.ClientCAs) || !equalPools(ctx.Config.RootCAs, tt.want.RootCAs) {
					t.Errorf("AddFederationToCAs() = %v, want %v", ctx.Config, tt.want)
				}
			}
		})
	}
}

func equalPools(a, b *x509.CertPool) bool {
	subjects := a.Subjects()
	sA := make([]string, len(subjects))
	for i := range subjects {
		sA[i] = string(subjects[i])
	}
	subjects = b.Subjects()
	sB := make([]string, len(subjects))
	for i := range subjects {
		sB[i] = string(subjects[i])
	}
	sort.Sort(sort.StringSlice(sA))
	sort.Sort(sort.StringSlice(sB))
	return reflect.DeepEqual(sA, sB)
}
