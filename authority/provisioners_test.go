package authority

import (
	"context"
	"crypto/x509"
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/smallstep/assert"
	"github.com/smallstep/certificates/api/render"
	"github.com/smallstep/certificates/authority/admin"
	"github.com/smallstep/certificates/authority/provisioner"
	"github.com/smallstep/certificates/db"
	"go.step.sm/crypto/jose"
	"go.step.sm/crypto/keyutil"
)

func TestGetEncryptedKey(t *testing.T) {
	type ek struct {
		a    *Authority
		kid  string
		err  error
		code int
	}
	tests := map[string]func(t *testing.T) *ek{
		"ok": func(t *testing.T) *ek {
			c, err := LoadConfiguration("../ca/testdata/ca.json")
			assert.FatalError(t, err)
			a, err := New(c)
			assert.FatalError(t, err)
			return &ek{
				a:   a,
				kid: c.AuthorityConfig.Provisioners[1].(*provisioner.JWK).Key.KeyID,
			}
		},
		"fail-not-found": func(t *testing.T) *ek {
			c, err := LoadConfiguration("../ca/testdata/ca.json")
			assert.FatalError(t, err)
			a, err := New(c)
			assert.FatalError(t, err)
			return &ek{
				a:    a,
				kid:  "foo",
				err:  errors.New("encrypted key with kid foo was not found"),
				code: http.StatusNotFound,
			}
		},
	}

	for name, genTestCase := range tests {
		t.Run(name, func(t *testing.T) {
			tc := genTestCase(t)

			ek, err := tc.a.GetEncryptedKey(tc.kid)
			if err != nil {
				if assert.NotNil(t, tc.err) {
					sc, ok := err.(render.StatusCodedError)
					assert.Fatal(t, ok, "error does not implement StatusCodedError interface")
					assert.Equals(t, sc.StatusCode(), tc.code)
					assert.HasPrefix(t, err.Error(), tc.err.Error())
				}
			} else {
				if assert.Nil(t, tc.err) {
					val, ok := tc.a.provisioners.Load("mike:" + tc.kid)
					assert.Fatal(t, ok)
					p, ok := val.(*provisioner.JWK)
					assert.Fatal(t, ok)
					assert.Equals(t, p.EncryptedKey, ek)
				}
			}
		})
	}
}

type mockAdminDB struct {
	admin.MockDB
	MGetCertificateData func(string) (*db.CertificateData, error)
}

func (c *mockAdminDB) GetCertificateData(sn string) (*db.CertificateData, error) {
	return c.MGetCertificateData(sn)
}

func TestGetProvisioners(t *testing.T) {
	type gp struct {
		a    *Authority
		err  error
		code int
	}
	tests := map[string]func(t *testing.T) *gp{
		"ok": func(t *testing.T) *gp {
			c, err := LoadConfiguration("../ca/testdata/ca.json")
			assert.FatalError(t, err)
			a, err := New(c)
			assert.FatalError(t, err)
			return &gp{a: a}
		},
	}

	for name, genTestCase := range tests {
		t.Run(name, func(t *testing.T) {
			tc := genTestCase(t)

			ps, next, err := tc.a.GetProvisioners("", 0)
			if err != nil {
				if assert.NotNil(t, tc.err) {
					sc, ok := err.(render.StatusCodedError)
					assert.Fatal(t, ok, "error does not implement StatusCodedError interface")
					assert.Equals(t, sc.StatusCode(), tc.code)
					assert.HasPrefix(t, err.Error(), tc.err.Error())
				}
			} else {
				if assert.Nil(t, tc.err) {
					assert.Equals(t, ps, tc.a.config.AuthorityConfig.Provisioners)
					assert.Equals(t, "", next)
				}
			}
		})
	}
}

func TestAuthority_LoadProvisionerByCertificate(t *testing.T) {
	_, priv, err := keyutil.GenerateDefaultKeyPair()
	assert.FatalError(t, err)
	csr := getCSR(t, priv)

	sign := func(a *Authority, extraOpts ...provisioner.SignOption) *x509.Certificate {
		key, err := jose.ReadKey("testdata/secrets/step_cli_key_priv.jwk", jose.WithPassword([]byte("pass")))
		assert.FatalError(t, err)
		token, err := generateToken("smallstep test", "step-cli", testAudiences.Sign[0], []string{"test.smallstep.com"}, time.Now(), key)
		assert.FatalError(t, err)
		ctx := provisioner.NewContextWithMethod(context.Background(), provisioner.SignMethod)
		opts, err := a.Authorize(ctx, token)
		assert.FatalError(t, err)
		opts = append(opts, extraOpts...)
		certs, err := a.Sign(csr, provisioner.SignOptions{}, opts...)
		assert.FatalError(t, err)
		return certs[0]
	}
	getProvisioner := func(a *Authority, name string) provisioner.Interface {
		p, ok := a.provisioners.LoadByName(name)
		if !ok {
			t.Fatalf("provisioner %s does not exists", name)
		}
		return p
	}
	removeExtension := provisioner.CertificateEnforcerFunc(func(cert *x509.Certificate) error {
		for i, ext := range cert.ExtraExtensions {
			if ext.Id.Equal(provisioner.StepOIDProvisioner) {
				cert.ExtraExtensions = append(cert.ExtraExtensions[:i], cert.ExtraExtensions[i+1:]...)
				break
			}
		}
		return nil
	})

	a0 := testAuthority(t)

	a1 := testAuthority(t)
	a1.db = &db.MockAuthDB{
		MUseToken: func(id, tok string) (bool, error) {
			return true, nil
		},
		MGetCertificateData: func(serialNumber string) (*db.CertificateData, error) {
			p, err := a1.LoadProvisionerByName("dev")
			if err != nil {
				t.Fatal(err)
			}
			return &db.CertificateData{
				Provisioner: &db.ProvisionerData{
					ID:   p.GetID(),
					Name: p.GetName(),
					Type: p.GetType().String(),
				},
			}, nil
		},
	}

	a2 := testAuthority(t)
	a2.adminDB = &mockAdminDB{
		MGetCertificateData: (func(s string) (*db.CertificateData, error) {
			p, err := a2.LoadProvisionerByName("dev")
			if err != nil {
				t.Fatal(err)
			}
			return &db.CertificateData{
				Provisioner: &db.ProvisionerData{
					ID:   p.GetID(),
					Name: p.GetName(),
					Type: p.GetType().String(),
				},
			}, nil
		}),
	}

	a3 := testAuthority(t)
	a3.db = &db.MockAuthDB{
		MUseToken: func(id, tok string) (bool, error) {
			return true, nil
		},
		MGetCertificateData: func(serialNumber string) (*db.CertificateData, error) {
			return &db.CertificateData{
				Provisioner: &db.ProvisionerData{
					ID: "foo", Name: "foo", Type: "foo",
				},
			}, nil
		},
	}

	a4 := testAuthority(t)
	a4.adminDB = &mockAdminDB{
		MGetCertificateData: func(serialNumber string) (*db.CertificateData, error) {
			return &db.CertificateData{
				Provisioner: &db.ProvisionerData{
					ID: "foo", Name: "foo", Type: "foo",
				},
			}, nil
		},
	}

	type args struct {
		crt *x509.Certificate
	}
	tests := []struct {
		name      string
		authority *Authority
		args      args
		want      provisioner.Interface
		wantErr   bool
	}{
		{"ok from certificate", a0, args{sign(a0)}, getProvisioner(a0, "step-cli"), false},
		{"ok from db", a1, args{sign(a1)}, getProvisioner(a1, "dev"), false},
		{"ok from admindb", a2, args{sign(a2)}, getProvisioner(a2, "dev"), false},
		{"fail from certificate", a0, args{sign(a0, removeExtension)}, nil, true},
		{"fail from db", a3, args{sign(a3, removeExtension)}, nil, true},
		{"fail from admindb", a4, args{sign(a4, removeExtension)}, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.authority.LoadProvisionerByCertificate(tt.args.crt)
			if (err != nil) != tt.wantErr {
				t.Errorf("Authority.LoadProvisionerByCertificate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Authority.LoadProvisionerByCertificate() = %v, want %v", got, tt.want)
			}
		})
	}
}
