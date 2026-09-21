package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"log"
	"strings"
	"testing"
	"time"

	"code.crute.us/mcrute/ses-smtpd-proxy/smtpd"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/ses"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	ststypes "github.com/aws/aws-sdk-go-v2/service/sts/types"
	"github.com/aws/smithy-go"
)

type fakeSTSClient struct {
	assumeRoleCalls     int
	assumeRoleErr       error
	assumeRoleOutputs   []*sts.AssumeRoleOutput
	callerIdentityCalls int
	callerIdentityErr   error
	callerIdentityOuts  []*sts.GetCallerIdentityOutput
	lastAssumeRoleInput *sts.AssumeRoleInput
}

func (c *fakeSTSClient) AssumeRole(_ context.Context, input *sts.AssumeRoleInput, _ ...func(*sts.Options)) (*sts.AssumeRoleOutput, error) {
	c.assumeRoleCalls++
	c.lastAssumeRoleInput = input
	if c.assumeRoleErr != nil {
		return nil, c.assumeRoleErr
	}
	if c.assumeRoleCalls > len(c.assumeRoleOutputs) {
		return nil, errors.New("unexpected AssumeRole call")
	}
	return c.assumeRoleOutputs[c.assumeRoleCalls-1], nil
}

func (c *fakeSTSClient) GetCallerIdentity(_ context.Context, _ *sts.GetCallerIdentityInput, _ ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
	c.callerIdentityCalls++
	if c.callerIdentityErr != nil {
		return nil, c.callerIdentityErr
	}
	if c.callerIdentityCalls > len(c.callerIdentityOuts) {
		return nil, errors.New("unexpected GetCallerIdentity call")
	}
	return c.callerIdentityOuts[c.callerIdentityCalls-1], nil
}

type fakeSESClient struct {
	ctx   context.Context
	input *ses.SendRawEmailInput
	err   error
}

func (c *fakeSESClient) SendRawEmail(ctx context.Context, input *ses.SendRawEmailInput, _ ...func(*ses.Options)) (*ses.SendRawEmailOutput, error) {
	c.ctx = ctx
	c.input = input
	return &ses.SendRawEmailOutput{}, c.err
}

var (
	_ stscreds.AssumeRoleAPIClient = (*fakeSTSClient)(nil)
	_ getCallerIdentityAPI         = (*fakeSTSClient)(nil)
	_ sesAPI                       = (*fakeSESClient)(nil)
)

func testAWSConfig() aws.Config {
	return aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("base-access-key", "base-secret", ""),
	}
}

func assumeRoleOutput(accessKeyID string, expires time.Time) *sts.AssumeRoleOutput {
	return &sts.AssumeRoleOutput{
		Credentials: &ststypes.Credentials{
			AccessKeyId:     aws.String(accessKeyID),
			SecretAccessKey: aws.String("assumed-secret"),
			SessionToken:    aws.String("session-token"),
			Expiration:      &expires,
		},
	}
}

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()

	var output bytes.Buffer
	writer := log.Writer()
	flags := log.Flags()
	prefix := log.Prefix()
	log.SetOutput(&output)
	log.SetFlags(0)
	log.SetPrefix("")
	t.Cleanup(func() {
		log.SetOutput(writer)
		log.SetFlags(flags)
		log.SetPrefix(prefix)
	})
	return &output
}

func testClientFactory(stsClient *fakeSTSClient, sesClient *fakeSESClient, sesConfig *aws.Config) awsClientFactory {
	return awsClientFactory{
		newSES: func(cfg aws.Config) sesAPI {
			*sesConfig = cfg
			return sesClient
		},
		newSTS: func(aws.Config) stsAPI {
			return stsClient
		},
	}
}

func TestMakeSesClientUsesAssumedCredentials(t *testing.T) {
	logs := captureLogs(t)
	ctx := context.Background()
	expires := time.Now().UTC().Add(time.Hour)
	roleARN := "arn:aws:iam::123456789012:role/ses-sender"
	stsClient := &fakeSTSClient{
		assumeRoleOutputs: []*sts.AssumeRoleOutput{
			assumeRoleOutput("assumed-access-key", expires),
		},
		callerIdentityOuts: []*sts.GetCallerIdentityOutput{
			{Arn: aws.String("arn:aws:iam::123456789012:role/base")},
			{Arn: aws.String("arn:aws:sts::123456789012:assumed-role/ses-sender/proxy")},
		},
	}
	sesClient := &fakeSESClient{}
	var sesConfig aws.Config

	client, err := makeSesClient(ctx, roleARN, "custom-session", func(context.Context) (aws.Config, error) {
		return testAWSConfig(), nil
	}, testClientFactory(stsClient, sesClient, &sesConfig))
	if err != nil {
		t.Fatalf("makeSesClient() error = %v", err)
	}
	if client != sesClient {
		t.Fatal("makeSesClient() did not return the injected SES client")
	}
	if stsClient.assumeRoleCalls != 1 {
		t.Fatalf("AssumeRole calls = %d, want 1", stsClient.assumeRoleCalls)
	}
	if got := aws.ToString(stsClient.lastAssumeRoleInput.RoleArn); got != roleARN {
		t.Fatalf("AssumeRole RoleArn = %q, want %q", got, roleARN)
	}
	if got := aws.ToString(stsClient.lastAssumeRoleInput.RoleSessionName); got != "custom-session" {
		t.Fatalf("AssumeRole RoleSessionName = %q, want %q", got, "custom-session")
	}

	assumedCredentials, err := sesConfig.Credentials.Retrieve(ctx)
	if err != nil {
		t.Fatalf("retrieve assumed credentials: %v", err)
	}
	if got := assumedCredentials.AccessKeyID; got != "assumed-access-key" {
		t.Fatalf("SES credentials AccessKeyID = %q, want assumed credentials", got)
	}

	for _, want := range []string{
		"assume-role: base identity arn:aws:iam::123456789012:role/base",
		"assume-role: assuming arn:aws:iam::123456789012:role/ses-sender (session custom-session)",
		"assume-role: refreshed credentials for arn:aws:iam::123456789012:role/ses-sender",
		"assume-role: assumed arn:aws:sts::123456789012:assumed-role/ses-sender/proxy, credentials expire " + expires.Format(time.RFC3339),
	} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("log output %q does not contain %q", logs.String(), want)
		}
	}
}

func TestAssumedCredentialsRefreshAfterExpiry(t *testing.T) {
	ctx := context.Background()
	stsClient := &fakeSTSClient{
		assumeRoleOutputs: []*sts.AssumeRoleOutput{
			assumeRoleOutput("first-access-key", time.Now().UTC().Add(30*time.Second)),
			assumeRoleOutput("refreshed-access-key", time.Now().UTC().Add(time.Hour)),
		},
		callerIdentityOuts: []*sts.GetCallerIdentityOutput{
			{Arn: aws.String("arn:aws:iam::123456789012:role/base")},
			{Arn: aws.String("arn:aws:sts::123456789012:assumed-role/ses-sender/proxy")},
		},
	}
	sesClient := &fakeSESClient{}
	var sesConfig aws.Config

	_, err := makeSesClient(ctx, "arn:aws:iam::123456789012:role/ses-sender", "ses-smtpd-proxy", func(context.Context) (aws.Config, error) {
		return testAWSConfig(), nil
	}, testClientFactory(stsClient, sesClient, &sesConfig))
	if err != nil {
		t.Fatalf("makeSesClient() error = %v", err)
	}
	if stsClient.assumeRoleCalls != 1 {
		t.Fatalf("initial AssumeRole calls = %d, want 1", stsClient.assumeRoleCalls)
	}

	credentials, err := sesConfig.Credentials.Retrieve(ctx)
	if err != nil {
		t.Fatalf("retrieve refreshed credentials: %v", err)
	}
	if stsClient.assumeRoleCalls != 2 {
		t.Fatalf("AssumeRole calls after expiry = %d, want 2", stsClient.assumeRoleCalls)
	}
	if got := credentials.AccessKeyID; got != "refreshed-access-key" {
		t.Fatalf("refreshed AccessKeyID = %q, want %q", got, "refreshed-access-key")
	}
}

func TestMakeSesClientReturnsAssumeRoleError(t *testing.T) {
	logs := captureLogs(t)
	assumeRoleErr := &smithy.GenericAPIError{
		Code:    "AccessDenied",
		Message: "not authorized to assume role",
		Fault:   smithy.FaultClient,
	}
	stsClient := &fakeSTSClient{
		assumeRoleErr: assumeRoleErr,
		callerIdentityOuts: []*sts.GetCallerIdentityOutput{
			{Arn: aws.String("arn:aws:iam::123456789012:role/base")},
		},
	}
	var sesConfig aws.Config
	sesCreated := false
	clients := testClientFactory(stsClient, &fakeSESClient{}, &sesConfig)
	clients.newSES = func(aws.Config) sesAPI {
		sesCreated = true
		return &fakeSESClient{}
	}

	_, err := makeSesClient(context.Background(), "arn:aws:iam::123456789012:role/ses-sender", "ses-smtpd-proxy", func(context.Context) (aws.Config, error) {
		return testAWSConfig(), nil
	}, clients)
	if err == nil {
		t.Fatal("makeSesClient() error = nil, want AssumeRole error")
	}
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("makeSesClient() error = %v, want smithy API error", err)
	}
	if got := apiErr.ErrorCode(); got != "AccessDenied" {
		t.Fatalf("API error code = %q, want %q", got, "AccessDenied")
	}
	if sesCreated {
		t.Fatal("SES client was created after assume-role startup failure")
	}
	if want := "assume-role: ERROR refreshing credentials for arn:aws:iam::123456789012:role/ses-sender: api error AccessDenied: not authorized to assume role"; !strings.Contains(logs.String(), want) {
		t.Fatalf("log output %q does not contain %q", logs.String(), want)
	}
}

func TestEnvelopeReturnsSMTPErrorForSESAPIError(t *testing.T) {
	logs := captureLogs(t)
	sesClient := &fakeSESClient{
		err: &smithy.GenericAPIError{
			Code:    "Throttling",
			Message: "retry later",
			Fault:   smithy.FaultServer,
		},
	}
	envelope := &Envelope{
		from:   "sender@example.com",
		ctx:    context.Background(),
		client: sesClient,
		rcpts:  []string{"recipient@example.com"},
	}
	envelope.b.WriteString("From: sender@example.com\r\n\r\nmessage body\r\n")

	err := envelope.Close()
	if _, ok := err.(smtpd.SMTPError); !ok {
		t.Fatalf("Envelope.Close() error type = %T, want smtpd.SMTPError", err)
	}
	if got := err.Error(); got != "451 4.5.1 Temporary server error. Please try again later" {
		t.Fatalf("Envelope.Close() error = %q, want temporary SMTP error", got)
	}
	if sesClient.ctx == nil {
		t.Fatal("SendRawEmail was not called")
	}
	if _, ok := sesClient.ctx.Deadline(); !ok {
		t.Fatal("SendRawEmail context does not have a timeout")
	}
	if got := aws.ToString(sesClient.input.Source); got != "sender@example.com" {
		t.Fatalf("SendRawEmail Source = %q, want %q", got, "sender@example.com")
	}
	if got := sesClient.input.Destinations; len(got) != 1 || got[0] != "recipient@example.com" {
		t.Fatalf("SendRawEmail Destinations = %v, want recipient", got)
	}
	if got := string(sesClient.input.RawMessage.Data); got != "From: sender@example.com\r\n\r\nmessage body\r\n" {
		t.Fatalf("SendRawEmail RawMessage = %q, want message body", got)
	}
	if want := "ERROR: ses: Throttling: retry later"; !strings.Contains(logs.String(), want) {
		t.Fatalf("log output %q does not contain %q", logs.String(), want)
	}
}

func TestApplyFlagEnvironmentDefaults(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	disablePrometheus := fs.Bool("disable-prometheus", false, "")
	prometheusBind := fs.String("prometheus-bind", ":2501", "")

	err := applyFlagEnvironmentDefaults(fs, func(key string) (string, bool) {
		values := map[string]string{
			"SES_SMTPD_PROXY_DISABLE_PROMETHEUS": "true",
			"SES_SMTPD_PROXY_PROMETHEUS_BIND":    ":9300",
		}
		value, ok := values[key]
		return value, ok
	})
	if err != nil {
		t.Fatalf("applyFlagEnvironmentDefaults() error = %v", err)
	}

	if !*disablePrometheus {
		t.Fatal("disable-prometheus = false, want true")
	}
	if got := *prometheusBind; got != ":9300" {
		t.Fatalf("prometheus-bind = %q, want %q", got, ":9300")
	}
}

func TestApplyFlagEnvironmentDefaultsAllowsCLIOverride(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	disablePrometheus := fs.Bool("disable-prometheus", false, "")
	prometheusBind := fs.String("prometheus-bind", ":2501", "")

	err := applyFlagEnvironmentDefaults(fs, func(key string) (string, bool) {
		values := map[string]string{
			"SES_SMTPD_PROXY_DISABLE_PROMETHEUS": "true",
			"SES_SMTPD_PROXY_PROMETHEUS_BIND":    ":9300",
		}
		value, ok := values[key]
		return value, ok
	})
	if err != nil {
		t.Fatalf("applyFlagEnvironmentDefaults() error = %v", err)
	}

	if err := fs.Parse([]string{"--disable-prometheus=false", "--prometheus-bind=:9400"}); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if *disablePrometheus {
		t.Fatal("disable-prometheus = true, want false")
	}
	if got := *prometheusBind; got != ":9400" {
		t.Fatalf("prometheus-bind = %q, want %q", got, ":9400")
	}
}

func TestApplyFlagEnvironmentDefaultsReturnsErrorForInvalidValue(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.Bool("disable-prometheus", false, "")

	err := applyFlagEnvironmentDefaults(fs, func(key string) (string, bool) {
		if key == "SES_SMTPD_PROXY_DISABLE_PROMETHEUS" {
			return "not-a-bool", true
		}
		return "", false
	})
	if err == nil {
		t.Fatal("applyFlagEnvironmentDefaults() error = nil, want error")
	}
	if got := err.Error(); !strings.Contains(got, "SES_SMTPD_PROXY_DISABLE_PROMETHEUS") {
		t.Fatalf("error = %q, want env var name included", got)
	}
}

func TestDefaultAssumeRoleSessionNameUsesHostname(t *testing.T) {
	got := defaultAssumeRoleSessionName(func() (string, error) {
		return "smtp-proxy-host", nil
	})
	if got != "smtp-proxy-host" {
		t.Fatalf("defaultAssumeRoleSessionName() = %q, want %q", got, "smtp-proxy-host")
	}
}

func TestDefaultAssumeRoleSessionNameFallsBackOnHostnameError(t *testing.T) {
	got := defaultAssumeRoleSessionName(func() (string, error) {
		return "", errors.New("hostname unavailable")
	})
	if got != "ses-smtpd-proxy" {
		t.Fatalf("defaultAssumeRoleSessionName() = %q, want %q", got, "ses-smtpd-proxy")
	}
}

func TestDefaultAssumeRoleSessionNameFallsBackOnEmptyHostname(t *testing.T) {
	got := defaultAssumeRoleSessionName(func() (string, error) {
		return "", nil
	})
	if got != "ses-smtpd-proxy" {
		t.Fatalf("defaultAssumeRoleSessionName() = %q, want %q", got, "ses-smtpd-proxy")
	}
}
