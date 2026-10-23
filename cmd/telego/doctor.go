package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/scratch-net/telego/pkg/config"
	"github.com/scratch-net/telego/pkg/log"
	"github.com/scratch-net/telego/pkg/tlsfront"
)

// DoctorCmd verifies the configured TLS fallback endpoint.
type DoctorCmd struct {
	Config     string        `short:"c" help:"Path to config file" type:"existingfile" required:""`
	Timeout    time.Duration `help:"TLS diagnostic timeout (maximum 30s)" default:"10s"`
	CertSource bool          `help:"Verify the certificate source instead of the probe fallback endpoint"`
}

func (c *DoctorCmd) Run() error {
	if c.Timeout <= 0 || c.Timeout > 30*time.Second {
		return errors.New("doctor timeout must be greater than zero and at most 30s")
	}
	file, err := config.Load(c.Config)
	if err != nil {
		return err
	}
	cfg, err := file.ToGProxyConfig()
	if err != nil {
		return err
	}
	host, port, proxyProtocol, scope := cfg.SpliceHost, cfg.SplicePort, cfg.SpliceProxyProtocol, "probe_fallback"
	if c.CertSource {
		host, port, proxyProtocol, scope = cfg.CertHost, cfg.CertPort, 0, "certificate_source"
	}
	address := net.JoinHostPort(host, strconv.Itoa(port))
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()
	report, err := tlsfront.CheckCertificate(ctx, tlsfront.CertificateCheck{
		Address: address, ServerName: cfg.MaskHost, ProxyProtocol: proxyProtocol,
	})
	if err != nil {
		return fmt.Errorf("%s TLS endpoint %s (SNI %s): %w", scope, address, cfg.MaskHost, err)
	}
	log.Info().Str("scope", scope).Str("address", address).Str("server_name", cfg.MaskHost).
		Bool("verified", true).Str("subject", report.Subject).Str("issuer", report.Issuer).
		Time("not_before", report.NotBefore).Time("not_after", report.NotAfter).
		Msg("TLS endpoint certificate passed verification")
	return nil
}
