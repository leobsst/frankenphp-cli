package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/leobsst/frankenphp-cli/internal/core/dns"
	"github.com/leobsst/frankenphp-cli/internal/core/env"
	"github.com/leobsst/frankenphp-cli/internal/core/hosts"
	"github.com/leobsst/frankenphp-cli/internal/core/s3"
	"github.com/leobsst/frankenphp-cli/internal/core/store"
	"github.com/leobsst/frankenphp-cli/internal/i18n"
	"github.com/leobsst/frankenphp-cli/internal/ui"
)

// s3ReadyTimeout is how long start waits for the S3 server to answer.
const s3ReadyTimeout = 90 * time.Second

// --- Lifecycle ---------------------------------------------------------------

// syncS3 prepares the S3 server once its container runs: the node layout
// on first start, the global key allowed on every bucket, then the hosts
// entries and DNS records of the bucket hosts.
func (a *app) syncS3(ctx context.Context, dir string, e *env.Env, s *store.Store) error {
	a.log.Info(i18n.T("Waiting for the S3 server..."))
	g := s3.NewGarage(e)
	if err := g.WaitReady(ctx, s3ReadyTimeout); err != nil {
		return err
	}
	if err := g.EnsureLayout(ctx); err != nil {
		return err
	}
	id, err := e.Require(s3.AccessKeyKey)
	if err != nil {
		return err
	}
	secret, err := e.Require(s3.SecretKeyKey)
	if err != nil {
		return err
	}
	if err := g.EnsureGlobalKey(ctx, id, secret); err != nil {
		return err
	}
	return a.syncS3Hosts(ctx, dir, e, s, g)
}

// syncS3Hosts brings the hosts entries of the bucket hosts in line with
// the buckets of the server, caches the bucket names, and refreshes the
// DNS records.
func (a *app) syncS3Hosts(ctx context.Context, dir string, e *env.Env, s *store.Store, g *s3.Garage) error {
	names, err := g.BucketNames(ctx)
	if err != nil {
		return err
	}
	old, err := s.S3Buckets()
	if err != nil {
		return err
	}
	h := a.deps.Hosts(a.log)
	for _, b := range old {
		if !slices.Contains(names, b) {
			if _, err := h.RemoveEntry(ctx, "127.0.0.1", s3.BucketHost(b)); err != nil {
				return err
			}
		}
	}
	for _, b := range names {
		if _, err := h.AddEntry(ctx, "127.0.0.1", s3.BucketHost(b)); err != nil {
			return err
		}
	}
	if err := s.SetS3Buckets(names); err != nil {
		return err
	}
	if dns.Enabled(e) {
		return dns.Sync(dir, e, s)
	}
	return nil
}

// removeS3Hosts removes the hosts entries of the S3 host and of the cached
// bucket hosts, errors ignored. The cache is kept for the next start.
func (a *app) removeS3Hosts(ctx context.Context, h *hosts.Manager, s *store.Store) {
	names := []string{s3.Domain}
	buckets, _ := s.S3Buckets()
	for _, b := range buckets {
		names = append(names, s3.BucketHost(b))
	}
	for _, name := range names {
		if found, err := h.HasEntry("127.0.0.1", name); err == nil && found {
			_, _ = h.RemoveEntry(ctx, "127.0.0.1", name)
		}
	}
}

// logS3Endpoints shows where the S3 server answers.
func (a *app) logS3Endpoints(e *env.Env) {
	a.log.Info(i18n.Tf("S3 server: https://%s (apps: %s, region %s)", s3.Domain,
		s3.AppEndpoint(e, e.IsProduction()), s3.Region(e)))
	a.log.Info(i18n.T("  Credentials and .env settings: frankenmanager s3 info"))
}

// s3Session is what the bucket and key commands work with.
type s3Session struct {
	dir string
	e   *env.Env
	s   *store.Store
	g   *s3.Garage
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
	if !s3.Enabled(e) {
		return nil, i18n.NewError("S3 is disabled: run 'frankenmanager service enable s3' to use it.")
	}
	g := s3.NewGarage(e)
	if !g.Ready(ctx) {
		return nil, i18n.NewError("The S3 server is not running: start it with 'frankenmanager start'.")
	}
	s, err := a.openStore(dir)
	if err != nil {
		return nil, err
	}
	return &s3Session{dir: dir, e: e, s: s, g: g}, nil
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
	buckets, err := x.g.Buckets(ctx)
	if err != nil {
		return err
	}
	if len(buckets) == 0 {
		a.log.Info(i18n.T("No bucket. Create one with: frankenmanager s3 bucket create <name>"))
		return nil
	}
	global := x.e.Get(s3.AccessKeyKey)
	t := ui.Table{Title: i18n.T("S3 buckets"), Columns: []ui.Column{
		{Header: i18n.T("Bucket"), Color: ui.Cyan}, {Header: i18n.T("Public")}, {Header: i18n.T("Objects")},
		{Header: i18n.T("Public objects")}, {Header: i18n.T("Size")}, {Header: i18n.T("Bucket keys")},
		{Header: "URL", Faint: true},
	}}
	objectsDir := x.e.ResolveDir(s3.DirKey, s3.DefaultDir, x.dir)
	for _, b := range buckets {
		keys := 0
		for _, k := range b.Keys {
			if k.ID != global {
				keys++
			}
		}
		public := strconv.Itoa(s3.PublicObjects(objectsDir, b.ID))
		if b.WebsiteAccess {
			public = i18n.T("all")
		}
		t.Rows = append(t.Rows, []string{b.Name(), yesNo(b.WebsiteAccess), strconv.FormatInt(b.Objects, 10),
			public, humanBytes(b.Bytes), strconv.Itoa(keys), s3.PublicURL(b.Name())})
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
	global := x.e.Get(s3.AccessKeyKey)
	for _, name := range names {
		if _, err := x.g.BucketInfo(ctx, name); err == nil {
			a.log.Info(i18n.Tf("Bucket %s already exists.", name))
			continue
		}
		b, err := x.g.CreateBucket(ctx, name)
		if err != nil {
			return err
		}
		if global != "" {
			if err := x.g.Allow(ctx, b.ID, global, s3.Perms{Owner: true, Read: true, Write: true}); err != nil {
				return err
			}
		}
		if public {
			if err := x.g.SetPublic(ctx, b.ID, true); err != nil {
				return err
			}
		}
		a.log.Success(i18n.Tf("Bucket %s created: %s", name, s3.PublicURL(name)))
	}
	if public {
		a.warnPublic()
	}
	return a.syncS3Hosts(ctx, x.dir, x.e, x.s, x.g)
}

func (a *app) s3DeleteBuckets(ctx context.Context, names []string, force bool) (err error) {
	x, err := a.openS3(ctx)
	if err != nil {
		return err
	}
	defer closeStore(x.s, &err)
	buckets := make([]s3.Bucket, 0, len(names))
	for _, name := range names {
		b, err := x.g.BucketInfo(ctx, name)
		if err != nil {
			return err
		}
		if b.Objects > 0 && !force {
			return i18n.Errorf("Bucket '%s' is not empty (%d objects): use --force to empty and delete it.",
				name, b.Objects)
		}
		buckets = append(buckets, b)
	}
	keys, err := x.g.Keys(ctx)
	if err != nil {
		return err
	}
	for _, b := range buckets {
		name := b.Name()
		if b.Objects > 0 {
			n, err := s3.NewObjects(x.e).Empty(ctx, name)
			if err != nil {
				return err
			}
			a.log.Info(i18n.Tf("Bucket %s emptied (%d objects deleted).", name, n))
		}
		if err := x.g.DeleteBucket(ctx, b.ID); err != nil {
			return err
		}
		if err := s3.DropBucketACL(x.e.ResolveDir(s3.DirKey, s3.DefaultDir, x.dir), b.ID); err != nil {
			return err
		}
		// The keys made by `s3 key create` for this bucket
		for _, k := range keys {
			if strings.HasPrefix(k.Name, name+"/") {
				if err := x.g.DeleteKey(ctx, k.ID); err != nil {
					return err
				}
				a.log.Info(i18n.Tf("Access key %s (%s) deleted.", k.ID, k.Name))
			}
		}
		a.log.Success(i18n.Tf("Bucket %s deleted.", name))
	}
	return a.syncS3Hosts(ctx, x.dir, x.e, x.s, x.g)
}

func (a *app) s3EmptyBuckets(ctx context.Context, names []string, yes bool) (err error) {
	x, err := a.openS3(ctx)
	if err != nil {
		return err
	}
	defer closeStore(x.s, &err)
	for _, name := range names {
		if _, err := x.g.BucketInfo(ctx, name); err != nil {
			return err
		}
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
	for _, name := range names {
		n, err := s3.NewObjects(x.e).Empty(ctx, name)
		if err != nil {
			return err
		}
		a.log.Success(i18n.Tf("Bucket %s emptied (%d objects deleted).", name, n))
	}
	return nil
}

func (a *app) s3SetPublic(ctx context.Context, names []string, public bool) (err error) {
	x, err := a.openS3(ctx)
	if err != nil {
		return err
	}
	defer closeStore(x.s, &err)
	for _, name := range names {
		b, err := x.g.BucketInfo(ctx, name)
		if err != nil {
			return err
		}
		if err := x.g.SetPublic(ctx, b.ID, public); err != nil {
			return err
		}
		if public {
			a.log.Success(i18n.Tf("Bucket %s is public: %s/<key>", name, s3.PublicURL(name)))
		} else {
			a.log.Success(i18n.Tf("Bucket %s is private.", name))
		}
	}
	if public {
		a.warnPublic()
	}
	return nil
}

// warnPublic explains what a public bucket exposes.
func (a *app) warnPublic() {
	a.log.Warning(i18n.T("Every object of a public bucket can be read and downloaded by anyone reaching " +
		"this server, without credentials, even the ones stored as private. To make only some objects " +
		"public, keep the bucket private and store them with the public-read ACL (visibility 'public')."))
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
		return usageError{i18n.NewError("The key name must not be empty nor contain '/'.")}
	}
	x, err := a.openS3(ctx)
	if err != nil {
		return err
	}
	defer closeStore(x.s, &err)
	b, err := x.g.BucketInfo(ctx, bucket)
	if err != nil {
		return err
	}
	k, err := x.g.CreateKey(ctx, bucket+"/"+label)
	if err != nil {
		return err
	}
	perms := s3.Perms{Read: true, Write: !readOnly}
	if err := x.g.Allow(ctx, b.ID, k.ID, perms); err != nil {
		return err
	}
	a.log.Success(i18n.Tf("Access key created for bucket %s (%s).", bucket, perms))
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
	t := ui.Table{Title: i18n.T("S3 access keys"), Columns: []ui.Column{
		{Header: i18n.T("Access key ID"), Color: ui.Cyan}, {Header: i18n.T("Name")}, {Header: i18n.T("Buckets")},
	}}
	if bucket != "" {
		b, err := x.g.BucketInfo(ctx, bucket)
		if err != nil {
			return err
		}
		for _, k := range b.Keys {
			t.Rows = append(t.Rows, []string{k.ID, k.Name, bucket + " (" + k.Permissions.String() + ")"})
		}
	} else {
		keys, err := x.g.Keys(ctx)
		if err != nil {
			return err
		}
		for _, summary := range keys {
			k, err := x.g.KeyInfo(ctx, summary.ID)
			if err != nil {
				return err
			}
			var buckets []string
			for _, b := range k.Buckets {
				name := b.ID
				if len(b.GlobalAliases) > 0 {
					name = b.GlobalAliases[0]
				}
				buckets = append(buckets, name+" ("+b.Permissions.String()+")")
			}
			if k.ID == x.e.Get(s3.AccessKeyKey) {
				buckets = []string{i18n.T("every bucket (global key)")}
			}
			t.Rows = append(t.Rows, []string{k.ID, k.Name, strings.Join(buckets, ", ")})
		}
	}
	if len(t.Rows) == 0 {
		a.log.Info(i18n.T("No access key."))
		return nil
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
	k, err := x.g.KeyInfo(ctx, id)
	if s3.IsNotFound(err) {
		return i18n.Errorf("Access key '%s' not found.", id)
	} else if err != nil {
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
	for _, id := range ids {
		if id == x.e.Get(s3.AccessKeyKey) {
			return i18n.Errorf("%s is the global key: change S3_ACCESS_KEY_ID instead.", id)
		}
	}
	for _, id := range ids {
		if err := x.g.DeleteKey(ctx, id); s3.IsNotFound(err) {
			return i18n.Errorf("Access key '%s' not found.", id)
		} else if err != nil {
			return err
		}
		a.log.Success(i18n.Tf("Access key %s deleted.", id))
	}
	return nil
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
