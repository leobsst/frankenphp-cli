package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/leobsst/frankenphp-cli/internal/core/buckets"
	"github.com/leobsst/frankenphp-cli/internal/core/env"
	"github.com/leobsst/frankenphp-cli/internal/core/s3"
	"github.com/leobsst/frankenphp-cli/internal/core/store"
	"github.com/leobsst/frankenphp-cli/internal/i18n"
	"github.com/leobsst/frankenphp-cli/internal/ui"
)

// --- Lifecycle ---------------------------------------------------------------

// syncS3 prepares the S3 server once its container runs (see
// stack.Service.SyncS3).
func (a *app) syncS3(ctx context.Context, dir string, e *env.Env, s *store.Store) error {
	return a.stackService(dir, e, nil, s).SyncS3(ctx)
}

// logS3Endpoints shows where the S3 server answers.
func (a *app) logS3Endpoints(e *env.Env) {
	a.log.Info(i18n.Tf("S3 server: https://%s (apps: %s, region %s)", s3.Domain,
		s3.AppEndpoint(e, e.IsProduction()), s3.Region(e)))
	a.log.Info(i18n.T("  Credentials and .env settings: frankenmanager s3 info"))
}

// s3Session is what the bucket and key commands work with.
type s3Session struct {
	s   *store.Store
	svc *buckets.Service
}

// openS3 checks that the S3 server is enabled and answers. The caller
// closes the store.
func (a *app) openS3(ctx context.Context) (*s3Session, error) {
	dir, err := a.projectDir()
	if err != nil {
		return nil, err
	}
	e, err := a.loadEnv(dir)
	if err != nil {
		return nil, err
	}
	g, err := buckets.Open(ctx, e)
	if err != nil {
		return nil, err
	}
	s, err := a.openStore(dir)
	if err != nil {
		return nil, err
	}
	log := a.events()
	return &s3Session{s: s, svc: &buckets.Service{
		Dir: dir, Env: e, Store: s, Hosts: a.deps.Hosts(log), Garage: g, Log: log,
	}}, nil
}

// cachedBuckets returns the bucket names known from the last sync, for
// the shell completion.
func (a *app) cachedBuckets() []string {
	dir, err := a.projectDir()
	if err != nil {
		return nil
	}
	s, err := a.openStore(dir)
	if err != nil {
		return nil
	}
	defer func() { _ = s.Close() }()
	names, _ := s.S3Buckets()
	return names
}

func (a *app) completeBuckets(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return a.cachedBuckets(), cobra.ShellCompDirectiveNoFileComp
}

// --- Commands ------------------------------------------------------------------

func newS3Cmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "s3",
		Short: i18n.T("Manage the S3 server: buckets, access keys and credentials."),
		Long: i18n.T(`Manage the S3 server (Garage): buckets, access keys and credentials.

Turn it on with 'frankenmanager service enable s3'. It is served at
https://s3.frankenmanager.test (path style) and at
https://<bucket>.s3.frankenmanager.test (virtual-host style), to the
browsers and to the PHP apps alike.

Examples:
    frankenmanager s3 info                          # Endpoints, credentials, .env settings
    frankenmanager s3 bucket create media           # Create a bucket
    frankenmanager s3 bucket public media           # Let anyone read its objects
    frankenmanager s3 key create media --read-only  # An access key for this bucket only
    frankenmanager s3 aws s3 ls s3://media          # Run the AWS CLI
    frankenmanager s3 garage stats                  # Run Garage's CLI`),
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newS3InfoCmd(a), newS3BucketCmd(a), newS3KeyCmd(a), newS3AWSCmd(a), newS3GarageCmd(a))
	return cmd
}

func newS3InfoCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: i18n.T("Show the S3 endpoints, the global credentials and the .env settings."),
		Args:  noArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			dir, err := a.projectDir()
			if err != nil {
				return err
			}
			e, err := a.loadEnv(dir)
			if err != nil {
				return err
			}
			if !s3.Enabled(e) {
				a.log.Warning(i18n.T("S3 is disabled: run 'frankenmanager service enable s3' to use it."))
				return nil
			}
			id, secret := e.Get(s3.AccessKeyKey), e.Get(s3.SecretKeyKey)
			if id == "" || secret == "" {
				a.log.Warning(i18n.T("No S3 credentials yet: they are generated on the next start."))
			}
			c := a.con
			t := ui.Table{Title: i18n.T("S3 server"), Columns: []ui.Column{
				{Header: i18n.T("Setting"), Color: ui.Cyan}, {Header: i18n.T("Value")},
			}}
			t.Rows = [][]string{
				{i18n.T("Endpoint (path style)"), "https://" + s3.Domain},
				{i18n.T("Endpoint (virtual-host style)"), "https://<bucket>." + s3.Domain},
				{i18n.T("Endpoint for PHP apps"), s3.AppEndpoint(e, e.IsProduction())},
				{i18n.T("Endpoint without TLS"), s3.Endpoint(e)},
				{i18n.T("Region"), s3.Region(e)},
				{i18n.T("Access key ID"), id},
				{i18n.T("Secret access key"), secret},
				{i18n.T("Objects directory"), e.ResolveDir(s3.DirKey, s3.DefaultDir, dir)},
			}
			c.PrintTable(t)
			c.Println()
			c.Println(c.Bold(i18n.T("Laravel .env (replace <bucket>):")))
			c.Println(c.Dim(strings.Join([]string{
				"FILESYSTEM_DISK=s3",
				"AWS_ACCESS_KEY_ID=" + id,
				"AWS_SECRET_ACCESS_KEY=" + secret,
				"AWS_DEFAULT_REGION=" + s3.Region(e),
				"AWS_BUCKET=<bucket>",
				"AWS_ENDPOINT=" + s3.AppEndpoint(e, e.IsProduction()),
				"AWS_URL=" + s3.PublicURL("<bucket>"),
				"AWS_USE_PATH_STYLE_ENDPOINT=true",
			}, "\n")))
			c.Println()
			c.Println(c.Dim(i18n.T("The global key is allowed on every bucket. For a key limited to one bucket: " +
				"frankenmanager s3 key create <bucket>")))
			return nil
		},
	}
}

func newS3BucketCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bucket",
		Short: i18n.T("List, create, delete or empty S3 buckets, or make them public."),
		Args:  noArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	var public, force, yes bool
	create := &cobra.Command{
		Use:   "create NAME...",
		Short: i18n.T("Create buckets, allowed to the global key."),
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.s3CreateBuckets(cmd.Context(), args, public)
		},
	}
	create.Flags().BoolVar(&public, "public", false, i18n.T("Let anyone read the objects of the buckets"))

	del := &cobra.Command{
		Use:               "delete NAME...",
		Short:             i18n.T("Delete buckets, and the access keys created for them."),
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: a.completeBuckets,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.s3DeleteBuckets(cmd.Context(), args, force)
		},
	}
	del.Flags().BoolVarP(&force, "force", "f", false, i18n.T("Empty the buckets first when they hold objects"))

	empty := &cobra.Command{
		Use:               "empty NAME...",
		Short:             i18n.T("Delete every object of buckets."),
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: a.completeBuckets,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.s3EmptyBuckets(cmd.Context(), args, yes)
		},
	}
	empty.Flags().BoolVarP(&yes, "yes", "y", false, i18n.T("Do not ask for confirmation"))

	visibility := func(use, short string, on bool) *cobra.Command {
		return &cobra.Command{
			Use:               use + " NAME...",
			Short:             short,
			Args:              cobra.MinimumNArgs(1),
			ValidArgsFunction: a.completeBuckets,
			RunE: func(cmd *cobra.Command, args []string) error {
				return a.s3SetPublic(cmd.Context(), args, on)
			},
		}
	}
	list := &cobra.Command{
		Use:   "list",
		Short: i18n.T("List the buckets with their usage."),
		Args:  noArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return a.s3ListBuckets(cmd.Context()) },
	}
	cmd.AddCommand(list, create, del, empty,
		visibility("public", i18n.T("Let anyone read every object of buckets, without credentials."), true),
		visibility("private", i18n.T("Require credentials to read the objects of buckets."), false))
	return cmd
}

func (a *app) s3ListBuckets(ctx context.Context) (err error) {
	x, err := a.openS3(ctx)
	if err != nil {
		return err
	}
	defer closeStore(x.s, &err)
	list, err := x.svc.List(ctx)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		a.log.Info(i18n.T("No bucket. Create one with: frankenmanager s3 bucket create <name>"))
		return nil
	}
	t := ui.Table{Title: i18n.T("S3 buckets"), Columns: []ui.Column{
		{Header: i18n.T("Bucket"), Color: ui.Cyan}, {Header: i18n.T("Public")}, {Header: i18n.T("Objects")},
		{Header: i18n.T("Public objects")}, {Header: i18n.T("Size")}, {Header: i18n.T("Bucket keys")},
		{Header: "URL", Faint: true},
	}}
	for _, b := range list {
		public := i18n.T("all")
		if b.PublicObjects >= 0 {
			public = strconv.Itoa(b.PublicObjects)
		}
		t.Rows = append(t.Rows, []string{b.Name, yesNo(b.Public), strconv.FormatInt(b.Objects, 10),
			public, humanBytes(b.Bytes), strconv.Itoa(b.Keys), b.URL})
	}
	a.con.PrintTable(t)
	return nil
}

func (a *app) s3CreateBuckets(ctx context.Context, names []string, public bool) (err error) {
	for _, name := range names {
		if err := s3.ValidateBucket(name); err != nil {
			return usageError{err}
		}
	}
	x, err := a.openS3(ctx)
	if err != nil {
		return err
	}
	defer closeStore(x.s, &err)
	_, err = x.svc.Create(ctx, names, public)
	return err
}

func (a *app) s3DeleteBuckets(ctx context.Context, names []string, force bool) (err error) {
	x, err := a.openS3(ctx)
	if err != nil {
		return err
	}
	defer closeStore(x.s, &err)
	return x.svc.Delete(ctx, names, force)
}

func (a *app) s3EmptyBuckets(ctx context.Context, names []string, yes bool) (err error) {
	x, err := a.openS3(ctx)
	if err != nil {
		return err
	}
	defer closeStore(x.s, &err)
	if err := x.svc.Check(ctx, names); err != nil {
		return err
	}
	if !yes {
		ok, err := a.confirm(i18n.Tf("Delete every object of %s? This cannot be undone.", strings.Join(names, ", ")))
		if errors.Is(err, errAborted) {
			fmt.Fprintln(a.errOut)
			fmt.Fprintln(a.errOut, i18n.T("Aborted!"))
			return exitError(1)
		}
		if err != nil {
			return err
		}
		if !ok {
			a.log.Info(i18n.T("Cancelled."))
			return nil
		}
	}
	return x.svc.Empty(ctx, names)
}

func (a *app) s3SetPublic(ctx context.Context, names []string, public bool) (err error) {
	x, err := a.openS3(ctx)
	if err != nil {
		return err
	}
	defer closeStore(x.s, &err)
	return x.svc.SetPublic(ctx, names, public)
}

func newS3KeyCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "key",
		Short: i18n.T("List, create, show or delete the access keys of buckets."),
		Args:  noArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	var label string
	var readOnly bool
	create := &cobra.Command{
		Use:               "create BUCKET",
		Short:             i18n.T("Create an access key allowed on one bucket only."),
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: a.completeBuckets,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.s3CreateKey(cmd.Context(), args[0], label, readOnly)
		},
	}
	create.Flags().StringVar(&label, "name", "app", i18n.T("Name of the key, stored as <bucket>/<name>"))
	create.Flags().BoolVar(&readOnly, "read-only", false, i18n.T("Allow reading only, not writing"))

	list := &cobra.Command{
		Use:               "list [BUCKET]",
		Short:             i18n.T("List the access keys, or the ones allowed on a bucket."),
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: a.completeBuckets,
		RunE: func(cmd *cobra.Command, args []string) error {
			bucket := ""
			if len(args) == 1 {
				bucket = args[0]
			}
			return a.s3ListKeys(cmd.Context(), bucket)
		},
	}
	show := &cobra.Command{
		Use:   "show ACCESS_KEY_ID",
		Short: i18n.T("Show an access key, its secret and its buckets."),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.s3ShowKey(cmd.Context(), args[0])
		},
	}
	del := &cobra.Command{
		Use:   "delete ACCESS_KEY_ID...",
		Short: i18n.T("Delete access keys."),
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.s3DeleteKeys(cmd.Context(), args)
		},
	}
	cmd.AddCommand(list, create, show, del)
	return cmd
}

func (a *app) s3CreateKey(ctx context.Context, bucket, label string, readOnly bool) (err error) {
	label = strings.TrimSpace(label)
	if label == "" || strings.Contains(label, "/") {
		return usageError{buckets.ErrKeyName}
	}
	x, err := a.openS3(ctx)
	if err != nil {
		return err
	}
	defer closeStore(x.s, &err)
	k, err := x.svc.CreateKey(ctx, bucket, label, readOnly)
	if err != nil {
		return err
	}
	a.log.Success(i18n.Tf("Access key created for bucket %s (%s).", bucket, k.Perms))
	a.printKey(k.ID, k.Name, k.Secret)
	return nil
}

func (a *app) printKey(id, name, secret string) {
	c := a.con
	t := ui.Table{Columns: []ui.Column{{Header: i18n.T("Setting"), Color: ui.Cyan}, {Header: i18n.T("Value")}}}
	t.Rows = [][]string{
		{i18n.T("Name"), name},
		{"AWS_ACCESS_KEY_ID", id},
		{"AWS_SECRET_ACCESS_KEY", secret},
	}
	c.PrintTable(t)
}

func (a *app) s3ListKeys(ctx context.Context, bucket string) (err error) {
	x, err := a.openS3(ctx)
	if err != nil {
		return err
	}
	defer closeStore(x.s, &err)
	keys, err := x.svc.Keys(ctx, bucket)
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		a.log.Info(i18n.T("No access key."))
		return nil
	}
	t := ui.Table{Title: i18n.T("S3 access keys"), Columns: []ui.Column{
		{Header: i18n.T("Access key ID"), Color: ui.Cyan}, {Header: i18n.T("Name")}, {Header: i18n.T("Buckets")},
	}}
	for _, k := range keys {
		var grants []string
		for _, g := range k.Grants {
			grants = append(grants, g.Bucket+" ("+g.Perms.String()+")")
		}
		if k.Global {
			grants = []string{i18n.T("every bucket (global key)")}
		}
		t.Rows = append(t.Rows, []string{k.ID, k.Name, strings.Join(grants, ", ")})
	}
	a.con.PrintTable(t)
	return nil
}

func (a *app) s3ShowKey(ctx context.Context, id string) (err error) {
	x, err := a.openS3(ctx)
	if err != nil {
		return err
	}
	defer closeStore(x.s, &err)
	k, err := x.svc.ShowKey(ctx, id)
	if err != nil {
		return err
	}
	a.printKey(k.ID, k.Name, k.Secret)
	return nil
}

func (a *app) s3DeleteKeys(ctx context.Context, ids []string) (err error) {
	x, err := a.openS3(ctx)
	if err != nil {
		return err
	}
	defer closeStore(x.s, &err)
	return x.svc.DeleteKeys(ctx, ids)
}

// passthrough reads the arguments of a command without flag parsing:
// the root options are dropped, and --help, -h or no argument shows its
// help (help is true).
func passthrough(args []string) (rest []string, help bool) {
	_, args = stripRootOptions(args)
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		return nil, true
	}
	if args[0] == "--" {
		args = args[1:]
	}
	return args, len(args) == 0
}

func newS3AWSCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "aws ARGS...",
		Short: i18n.T("Run the AWS CLI against the S3 server, with the global key."),
		Long: i18n.T(`Run the AWS CLI against the S3 server, with the global key.

The CLI runs in a one-off container (amazon/aws-cli, downloaded on first
use), already set up: endpoint, region, credentials and path style. The
current directory is its working directory, so that the local paths given
to 'aws s3 cp' or 'aws s3 sync' resolve; paths outside of it do not.

Examples:
    frankenmanager s3 aws s3 ls
    frankenmanager s3 aws s3 ls s3://media --recursive
    frankenmanager s3 aws s3 cp ./logo.png s3://media/img/logo.png
    frankenmanager s3 aws s3 sync ./public/uploads s3://media/uploads
    frankenmanager s3 aws s3api head-object --bucket media --key img/logo.png`),
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			args, help := passthrough(args)
			if help {
				return cmd.Help()
			}
			return a.s3AWS(cmd.Context(), args)
		},
	}
}

func newS3GarageCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "garage ARGS...",
		Short: i18n.T("Run Garage's own CLI in the S3 container."),
		Long: i18n.T(`Run Garage's own CLI in the S3 container, for what the s3 commands do not
cover: statistics, bucket details and aliases, quotas, repairs...

Examples:
    frankenmanager s3 garage status
    frankenmanager s3 garage stats
    frankenmanager s3 garage bucket info media
    frankenmanager s3 garage bucket set-quotas media --max-size 1GiB
    frankenmanager s3 garage help`),
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			args, help := passthrough(args)
			if help {
				return cmd.Help()
			}
			return a.s3Garage(cmd.Context(), args)
		},
	}
}

// s3Runnable loads the configuration and checks that the S3 server runs.
func (a *app) s3Runnable(ctx context.Context) (string, *env.Env, error) {
	dir, err := a.projectDir()
	if err != nil {
		return "", nil, err
	}
	e, err := a.loadEnv(dir)
	if err != nil {
		return "", nil, err
	}
	if !s3.Enabled(e) {
		return "", nil, i18n.NewError("S3 is disabled: run 'frankenmanager service enable s3' to use it.")
	}
	if !s3.NewGarage(e).Ready(ctx) {
		return "", nil, i18n.NewError("The S3 server is not running: start it with 'frankenmanager start'.")
	}
	return dir, e, nil
}

// runDocker runs docker on the terminal, its exit code becoming ours.
func (a *app) runDocker(ctx context.Context, args []string) error {
	code, err := a.deps.Interactive(ctx, "docker", args)
	if err != nil {
		return err
	}
	if code != 0 {
		return exitError(code)
	}
	return nil
}

func (a *app) s3Garage(ctx context.Context, args []string) error {
	if _, _, err := a.s3Runnable(ctx); err != nil {
		return err
	}
	return a.runDocker(ctx, s3.GarageExecArgs(args, a.deps.TTY()))
}

func (a *app) s3AWS(ctx context.Context, args []string) error {
	dir, e, err := a.s3Runnable(ctx)
	if err != nil {
		return err
	}
	if e.Get(s3.AccessKeyKey) == "" || e.Get(s3.SecretKeyKey) == "" {
		return i18n.NewError("No S3 credentials yet: they are generated on the next start.")
	}
	config, err := s3.WriteAWSConfig(dir, e)
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	if real, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = real
	}
	return a.runDocker(ctx, s3.AWSRunArgs(config, cwd, containerPath(cwd), execUser(runtime.GOOS, e), args, a.deps.TTY()))
}

// humanBytes formats a size: 0 B, 1.5 KB, 12.0 MB...
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
