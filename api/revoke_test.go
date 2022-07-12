package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pkg/errors"

	"github.com/smallstep/assert"
	"github.com/smallstep/certificates/authority"
	"github.com/smallstep/certificates/authority/provisioner"
	"github.com/smallstep/certificates/errs"
	"github.com/smallstep/certificates/logging"
)

func TestRevokeRequestValidate(t *testing.T) {
	type test struct {
		rr  *RevokeRequest
		err *errs.Error
	}
	tests := map[string]test{
		"error/missing serial": {
			rr:  &RevokeRequest{},
			err: &errs.Error{Err: errors.New("missing serial"), Status: http.StatusBadRequest},
		},
		"error/bad reasonCode": {
			rr: &RevokeRequest{
				Serial:     "sn",
				ReasonCode: 15,
				Passive:    true,
			},
			err: &errs.Error{Err: errors.New("reasonCode out of bounds"), Status: http.StatusBadRequest},
		},
		"error/non-passive not implemented": {
			rr: &RevokeRequest{
				Serial:     "sn",
				ReasonCode: 8,
				Passive:    false,
			},
			err: &errs.Error{Err: errors.New("non-passive revocation not implemented"), Status: http.StatusNotImplemented},
		},
		"ok": {
			rr: &RevokeRequest{
				Serial:     "sn",
				ReasonCode: 9,
				Passive:    true,
			},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if err := tc.rr.Validate(); err != nil {
				switch v := err.(type) {
				case *errs.Error:
					assert.HasPrefix(t, v.Error(), tc.err.Error())
					assert.Equals(t, v.StatusCode(), tc.err.Status)
				default:
					t.Errorf("unexpected error type: %T", v)
				}
			} else {
				assert.Nil(t, tc.err)
			}
		})
	}
}

func Test_caHandler_Revoke(t *testing.T) {
	type test struct {
		input      string
		auth       Authority
		tls        *tls.ConnectionState
		statusCode int
		expected   []byte
	}
	tests := map[string]func(*testing.T) test{
		"400/json read error": func(t *testing.T) test {
			return test{
				input:      "{",
				statusCode: http.StatusBadRequest,
			}
		},
		"400/invalid request body": func(t *testing.T) test {
			input, err := json.Marshal(RevokeRequest{})
			assert.FatalError(t, err)
			return test{
				input:      string(input),
				statusCode: http.StatusBadRequest,
			}
		},
		"200/ott": func(t *testing.T) test {
			input, err := json.Marshal(RevokeRequest{
				Serial:     "sn",
				ReasonCode: 4,
				Reason:     "foo",
				OTT:        "valid",
				Passive:    true,
			})
			assert.FatalError(t, err)
			return test{
				input:      string(input),
				statusCode: http.StatusOK,
				auth: &mockAuthority{
					authorize: func(ctx context.Context, ott string) ([]provisioner.SignOption, error) {
						return nil, nil
					},
					revoke: func(ctx context.Context, opts *authority.RevokeOptions) error {
						assert.True(t, opts.PassiveOnly)
						assert.False(t, opts.MTLS)
						assert.Equals(t, opts.Serial, "sn")
						assert.Equals(t, opts.ReasonCode, 4)
						assert.Equals(t, opts.Reason, "foo")
						return nil
					},
				},
				expected: []byte(`{"status":"ok"}`),
			}
		},
		"400/no OTT and no peer certificate": func(t *testing.T) test {
			input, err := json.Marshal(RevokeRequest{
				Serial:     "sn",
				ReasonCode: 4,
				Passive:    true,
			})
			assert.FatalError(t, err)
			return test{
				input:      string(input),
				statusCode: http.StatusBadRequest,
			}
		},
		"200/no ott": func(t *testing.T) test {
			cs := &tls.ConnectionState{
				PeerCertificates: []*x509.Certificate{parseCertificate(certPEM)},
			}
			input, err := json.Marshal(RevokeRequest{
				Serial:     "1404354960355712309",
				ReasonCode: 4,
				Reason:     "foo",
				Passive:    true,
			})
			assert.FatalError(t, err)

			return test{
				input:      string(input),
				statusCode: http.StatusOK,
				tls:        cs,
				auth: &mockAuthority{
					authorize: func(ctx context.Context, ott string) ([]provisioner.SignOption, error) {
						return nil, nil
					},
					revoke: func(ctx context.Context, ri *authority.RevokeOptions) error {
						assert.True(t, ri.PassiveOnly)
						assert.True(t, ri.MTLS)
						assert.Equals(t, ri.Serial, "1404354960355712309")
						assert.Equals(t, ri.ReasonCode, 4)
						assert.Equals(t, ri.Reason, "foo")
						return nil
					},
					loadProvisionerByCertificate: func(crt *x509.Certificate) (provisioner.Interface, error) {
						return &mockProvisioner{
							getID: func() string {
								return "mock-provisioner-id"
							},
						}, err
					},
				},
				expected: []byte(`{"status":"ok"}`),
			}
		},
		"500/ott authority.Revoke": func(t *testing.T) test {
			input, err := json.Marshal(RevokeRequest{
				Serial:     "sn",
				ReasonCode: 4,
				Reason:     "foo",
				OTT:        "valid",
				Passive:    true,
			})
			assert.FatalError(t, err)
			return test{
				input:      string(input),
				statusCode: http.StatusInternalServerError,
				auth: &mockAuthority{
					authorize: func(ctx context.Context, ott string) ([]provisioner.SignOption, error) {
						return nil, nil
					},
					revoke: func(ctx context.Context, opts *authority.RevokeOptions) error {
						return errs.InternalServer("force")
					},
				},
			}
		},
		"403/ott authority.Revoke": func(t *testing.T) test {
			input, err := json.Marshal(RevokeRequest{
				Serial:     "sn",
				ReasonCode: 4,
				Reason:     "foo",
				OTT:        "valid",
				Passive:    true,
			})
			assert.FatalError(t, err)
			return test{
				input:      string(input),
				statusCode: http.StatusForbidden,
				auth: &mockAuthority{
					authorize: func(ctx context.Context, ott string) ([]provisioner.SignOption, error) {
						return nil, nil
					},
					revoke: func(ctx context.Context, opts *authority.RevokeOptions) error {
						return errors.New("force")
					},
				},
			}
		},
	}

	for name, _tc := range tests {
		tc := _tc(t)
		t.Run(name, func(t *testing.T) {
			mockMustAuthority(t, tc.auth)
			req := httptest.NewRequest("POST", "http://example.com/revoke", strings.NewReader(tc.input))
			if tc.tls != nil {
				req.TLS = tc.tls
			}
			w := httptest.NewRecorder()
			Revoke(logging.NewResponseLogger(w), req)
			res := w.Result()

			assert.Equals(t, tc.statusCode, res.StatusCode)

			body, err := io.ReadAll(res.Body)
			res.Body.Close()
			assert.FatalError(t, err)

			if tc.statusCode < http.StatusBadRequest {
				if !bytes.Equal(bytes.TrimSpace(body), tc.expected) {
					t.Errorf("caHandler.Root Body = %s, wants %s", body, tc.expected)
				}
			}
		})
	}
}
