package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	syncrunner "github.com/wechat-article/wechat-article-exporter/cli/internal/sync"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/wechat"
)

func (a *App) accountCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "account",
		Short: "Discover and manage local WeChat accounts",
		Example: `  wechat-article account search "keyword" --limit 20
  wechat-article account add fakeid --name "Account name"
  wechat-article account list --keyword "name" --json
  wechat-article account export --output accounts.json`,
	}

	var offset, limit int
	search := &cobra.Command{
		Use: "search <keyword>", Short: "Search authenticated WeChat account discovery", Args: exactArgs(1, "account search requires <keyword>"),
		RunE: func(command *cobra.Command, args []string) error {
			if err := validatePage(offset, limit); err != nil {
				return err
			}
			page, err := a.core.SearchAccounts(command.Context(), domain.AccountQuery{Keyword: args[0], Offset: offset, Limit: limit})
			if err != nil {
				return err
			}
			return a.output(page)
		},
	}
	addPageFlags(search, &offset, &limit, 20)

	var listKeyword string
	var listOffset, listLimit int
	list := &cobra.Command{
		Use: "list", Short: "List locally saved accounts", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if err := validatePage(listOffset, listLimit); err != nil {
				return err
			}
			page, err := a.core.QueryAccounts(command.Context(), domain.AccountQuery{Keyword: listKeyword, Offset: listOffset, Limit: listLimit})
			if err != nil {
				return err
			}
			return a.output(page)
		},
	}
	list.Flags().StringVar(&listKeyword, "keyword", "", "filter saved account names, aliases, or fakeids")
	addPageFlags(list, &listOffset, &listLimit, 50)

	var accountName, accountAlias, accountDescription, avatarURL string
	var serviceType int
	add := &cobra.Command{
		Use: "add <fakeid>", Short: "Add or conservatively merge an account into the local library", Args: exactArgs(1, "account add requires <fakeid>"),
		RunE: func(command *cobra.Command, args []string) error {
			if strings.TrimSpace(accountName) == "" {
				return usage("account add requires --name")
			}
			account, err := a.core.SaveAccount(command.Context(), domain.Account{
				FakeID: args[0], Name: accountName, Alias: accountAlias, Description: accountDescription,
				AvatarURL: avatarURL, ServiceType: serviceType,
			})
			if err != nil {
				return err
			}
			return a.output(account)
		},
	}
	addAccountFields(add, &accountName, &accountAlias, &accountDescription, &avatarURL, &serviceType)

	get := &cobra.Command{
		Use: "get <id>", Short: "Get a locally saved account by stable ID", Args: exactArgs(1, "account get requires <id>"),
		RunE: func(command *cobra.Command, args []string) error {
			account, err := a.core.GetAccount(command.Context(), domain.AccountID(args[0]))
			if err != nil {
				return err
			}
			return a.output(account)
		},
	}
	getFakeID := &cobra.Command{
		Use: "get-by-fakeid <fakeid>", Short: "Get a locally saved account by WeChat fakeid", Args: exactArgs(1, "account get-by-fakeid requires <fakeid>"),
		RunE: func(command *cobra.Command, args []string) error {
			account, err := a.core.GetAccountByFakeID(command.Context(), args[0])
			if err != nil {
				return err
			}
			return a.output(account)
		},
	}

	var updateName, updateAlias, updateDescription, updateAvatar string
	var updateServiceType int
	update := &cobra.Command{
		Use: "update <id>", Short: "Replace editable local account metadata", Args: exactArgs(1, "account update requires <id>"),
		RunE: func(command *cobra.Command, args []string) error {
			existing, err := a.core.GetAccount(command.Context(), domain.AccountID(args[0]))
			if err != nil {
				return err
			}
			if !command.Flags().Changed("name") && !command.Flags().Changed("alias") && !command.Flags().Changed("description") &&
				!command.Flags().Changed("avatar-url") && !command.Flags().Changed("service-type") {
				return usage("account update requires at least one editable field flag")
			}
			if command.Flags().Changed("name") {
				existing.Name = updateName
			}
			if command.Flags().Changed("alias") {
				existing.Alias = updateAlias
			}
			if command.Flags().Changed("description") {
				existing.Description = updateDescription
			}
			if command.Flags().Changed("avatar-url") {
				existing.AvatarURL = updateAvatar
			}
			if command.Flags().Changed("service-type") {
				existing.ServiceType = updateServiceType
			}
			updated, err := a.core.UpdateAccount(command.Context(), existing)
			if err != nil {
				return err
			}
			return a.output(updated)
		},
	}
	addAccountFields(update, &updateName, &updateAlias, &updateDescription, &updateAvatar, &updateServiceType)

	resolveName := a.unaryResultCommand("name <article-url>", "Resolve the publisher name from a WeChat article URL", "account name requires <article-url>", func(command *cobra.Command, value string) (any, error) {
		name, err := a.core.ResolveAccountName(command.Context(), value)
		return map[string]string{"name": name}, err
	})
	fromURL := a.unaryResultCommand("from-url <article-url>", "Resolve an account from a WeChat article URL", "account from-url requires <article-url>", func(command *cobra.Command, value string) (any, error) {
		return a.core.ResolveAccountFromArticle(command.Context(), value)
	})
	details := a.unaryResultCommand("details <fakeid>", "Get authenticated account details", "account details requires <fakeid>", func(command *cobra.Command, value string) (any, error) {
		return a.core.AccountDetails(command.Context(), value)
	})
	author := a.unaryResultCommand("author <fakeid>", "Get authenticated author information", "account author requires <fakeid>", func(command *cobra.Command, value string) (any, error) {
		return a.core.AuthorInfo(command.Context(), value)
	})

	var exportKeyword, exportOutput string
	exportCommand := &cobra.Command{
		Use: "export", Short: "Export saved accounts as a versioned manifest", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			manifest, err := a.core.ExportAccounts(command.Context(), domain.AccountQuery{Keyword: exportKeyword})
			if err != nil {
				return err
			}
			if exportOutput == "" || exportOutput == "-" {
				return a.output(manifest)
			}
			if err := writePrivateJSONFile(exportOutput, manifest); err != nil {
				return err
			}
			return a.output(map[string]any{"path": exportOutput, "count": len(manifest.Accounts), "schemaVersion": manifest.SchemaVersion})
		},
	}
	exportCommand.Flags().StringVar(&exportKeyword, "keyword", "", "filter accounts included in the manifest")
	exportCommand.Flags().StringVarP(&exportOutput, "output", "o", "", "write the manifest to a file; default stdout")

	var importInput string
	importCommand := &cobra.Command{
		Use: "import", Short: "Validate and merge a versioned account manifest", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			var manifest domain.AccountManifest
			if err := decodeJSONInput(importInput, a.stdin, &manifest); err != nil {
				return usage("decode account manifest: " + err.Error())
			}
			report, err := a.core.ImportAccounts(command.Context(), manifest)
			if err != nil {
				return err
			}
			return a.output(report)
		},
	}
	importCommand.Flags().StringVarP(&importInput, "file", "f", "-", "manifest file or - for stdin")

	var confirmation string
	deleteCommand := &cobra.Command{
		Use: "delete <id> [id...]", Short: "Transactionally delete account-scoped local data", Args: cobra.MinimumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			required := "delete-accounts:" + strings.Join(args, ",")
			if confirmation != required {
				return usage("account deletion removes account-scoped metadata and makes unreferenced objects GC-eligible; use --confirm " + required)
			}
			ids := make([]domain.AccountID, len(args))
			for index, id := range args {
				ids[index] = domain.AccountID(id)
			}
			report, err := a.core.DeleteAccounts(command.Context(), ids)
			if err != nil {
				return err
			}
			return a.output(report)
		},
	}
	deleteCommand.Flags().StringVar(&confirmation, "confirm", "", "exact confirmation value")

	command.AddCommand(search, list, add, get, getFakeID, update, resolveName, fromURL, details, author, importCommand, exportCommand, deleteCommand)
	return command
}

func (a *App) articleCommand() *cobra.Command {
	command := &cobra.Command{
		Use: "article", Short: "Query local articles and inspect upstream account article lists",
		Example: `  wechat-article article list --account account-id --keyword "AI" --sort published:desc
  wechat-article article discover fakeid --all --json`,
	}

	var query domain.ArticleQuery
	var publishedFrom, publishedTo, messageTypes, sorts string
	var deleted, hasContent, hasComments, original, paid string
	list := &cobra.Command{
		Use: "list", Short: "Query locally stored articles", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if err := validatePage(query.Offset, query.Limit); err != nil {
				return err
			}
			var err error
			query.PublishedFrom, err = parseOptionalTime(publishedFrom)
			if err != nil {
				return usage("--published-from: " + err.Error())
			}
			query.PublishedTo, err = parseOptionalTime(publishedTo)
			if err != nil {
				return usage("--published-to: " + err.Error())
			}
			if query.PublishedFrom.After(query.PublishedTo) && !query.PublishedTo.IsZero() {
				return usage("--published-from must not be after --published-to")
			}
			if query.Deleted, err = parseOptionalBool(deleted); err != nil {
				return usage("--deleted: " + err.Error())
			}
			if query.HasContent, err = parseOptionalBool(hasContent); err != nil {
				return usage("--has-content: " + err.Error())
			}
			if query.HasComments, err = parseOptionalBool(hasComments); err != nil {
				return usage("--has-comments: " + err.Error())
			}
			if query.Original, err = parseOptionalBool(original); err != nil {
				return usage("--original: " + err.Error())
			}
			if query.Paid, err = parseOptionalBool(paid); err != nil {
				return usage("--paid: " + err.Error())
			}
			if query.MessageTypes, err = parseInts(messageTypes); err != nil {
				return usage("--message-types: " + err.Error())
			}
			if query.Sorts, err = parseSorts(sorts); err != nil {
				return usage("--sort: " + err.Error())
			}
			page, err := a.core.QueryArticles(command.Context(), query)
			if err != nil {
				return err
			}
			return a.output(page)
		},
	}
	list.Flags().StringVar((*string)(&query.AccountID), "account", "", "filter by stable account ID")
	list.Flags().StringVar((*string)(&query.AlbumID), "album", "", "filter by stable album ID")
	list.Flags().StringVar(&query.Keyword, "keyword", "", "filter title or digest")
	list.Flags().StringVar(&query.Author, "author", "", "filter author")
	list.Flags().StringVar(&query.State, "state", "", "filter normalized state")
	list.Flags().StringVar(&publishedFrom, "published-from", "", "inclusive RFC3339 timestamp or YYYY-MM-DD")
	list.Flags().StringVar(&publishedTo, "published-to", "", "inclusive RFC3339 timestamp or YYYY-MM-DD")
	list.Flags().StringVar(&deleted, "deleted", "", "true or false")
	list.Flags().StringVar(&hasContent, "has-content", "", "true or false")
	list.Flags().StringVar(&hasComments, "has-comments", "", "true or false")
	list.Flags().StringVar(&original, "original", "", "true or false")
	list.Flags().StringVar(&paid, "paid", "", "true or false")
	list.Flags().StringVar(&messageTypes, "message-types", "", "comma-separated numeric WeChat message types")
	list.Flags().StringVar(&sorts, "sort", "published:desc", "comma-separated field:asc|desc sort keys")
	addPageFlags(list, &query.Offset, &query.Limit, 50)

	var keyword string
	var offset, limit int
	var all bool
	discover := &cobra.Command{
		Use: "discover <fakeid>", Short: "List normalized upstream article records for an account", Args: exactArgs(1, "article discover requires <fakeid>"),
		RunE: func(command *cobra.Command, args []string) error {
			if err := validatePage(offset, limit); err != nil {
				return err
			}
			request := wechat.ArticleListRequest{FakeID: args[0], Keyword: keyword, Offset: offset, Limit: limit}
			if !all {
				page, err := a.core.ListArticles(command.Context(), request)
				if err != nil {
					return err
				}
				return a.output(page)
			}
			result := wechat.ArticlePage{Items: []domain.Article{}, Offset: offset, Limit: limit}
			for {
				page, err := a.core.ListArticles(command.Context(), request)
				if err != nil {
					return err
				}
				result.Items = append(result.Items, page.Items...)
				result.Total, result.Next, result.Completed = page.Total, page.Next, page.Completed
				fmt.Fprintf(a.stderr, "article discovery: offset=%d items=%d total=%d completed=%t\n", request.Offset, len(page.Items), page.Total, page.Completed)
				if page.Completed || len(page.Items) == 0 {
					break
				}
				request.Offset = page.Next
			}
			return a.output(result)
		},
	}
	discover.Flags().StringVar(&keyword, "keyword", "", "filter upstream titles")
	addPageFlags(discover, &offset, &limit, 20)
	discover.Flags().BoolVar(&all, "all", false, "continue until upstream pagination completes")

	command.AddCommand(list, discover)
	return command
}

func (a *App) albumCommand() *cobra.Command {
	command := &cobra.Command{Use: "album", Short: "Query, traverse, and download locally known albums"}
	var accountID, keyword string
	var offset, limit int
	list := &cobra.Command{
		Use: "list", Short: "List locally stored album metadata", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if err := validatePage(offset, limit); err != nil {
				return err
			}
			page, err := a.core.QueryAlbums(command.Context(), domain.AlbumQuery{
				AccountID: domain.AccountID(accountID), Keyword: keyword, Offset: offset, Limit: limit,
			})
			if err != nil {
				return err
			}
			return a.output(page)
		},
	}
	list.Flags().StringVar(&accountID, "account", "", "filter by stable account ID")
	list.Flags().StringVar(&keyword, "keyword", "", "filter album title")
	addPageFlags(list, &offset, &limit, 50)

	var fakeID, order string
	var pageSize int
	var pageDelay time.Duration
	var downloadAfter bool
	var async asyncOptions
	traverse := &cobra.Command{
		Use: "traverse <album-id>", Short: "Traverse all album pages in a resumable persistent job", Args: exactArgs(1, "album traverse requires <album-id>"),
		Example: `  wechat-article album traverse ALBUM_ID --fakeid ACCOUNT_FAKEID --order forward --follow
  wechat-article album traverse ALBUM_ID --fakeid ACCOUNT_FAKEID --download --wait`,
		RunE: func(command *cobra.Command, args []string) error {
			if err := async.validate(); err != nil {
				return err
			}
			if strings.TrimSpace(fakeID) == "" {
				return usage("album traverse requires --fakeid")
			}
			if order != string(wechat.AlbumForward) && order != string(wechat.AlbumReverse) {
				return usage("--order must be forward or reverse")
			}
			if pageSize < 1 || pageSize > 50 {
				return usage("--page-size must be between 1 and 50")
			}
			if pageDelay < 0 {
				return usage("--page-delay must be non-negative")
			}
			runtime, ok := a.active.Syncs.(*localSyncRuntime)
			if !ok || runtime == nil {
				return errors.New("local album sync runtime is unavailable")
			}
			job, err := runtime.StartAlbum(command.Context(), syncrunner.AlbumSyncRequest{
				FakeID: fakeID, AlbumID: args[0], Order: wechat.AlbumOrder(order), PageSize: pageSize, PageDelay: pageDelay,
			}, downloadAfter)
			if err != nil {
				return err
			}
			return a.outputJob(command.Context(), job, async)
		},
	}
	traverse.Flags().StringVar(&fakeID, "fakeid", "", "publisher fakeid used by the authenticated album endpoint")
	traverse.Flags().StringVar(&order, "order", string(wechat.AlbumForward), "forward or reverse traversal order")
	traverse.Flags().IntVar(&pageSize, "page-size", 20, "upstream page size between 1 and 50")
	traverse.Flags().DurationVar(&pageDelay, "page-delay", 5*time.Second, "delay between album pages")
	traverse.Flags().BoolVar(&downloadAfter, "download", false, "record that stored album articles should be batch-downloaded after traversal")
	async.addFlags(traverse)

	command.AddCommand(list, traverse)
	return command
}

func (a *App) unaryResultCommand(use, short, message string, operation func(*cobra.Command, string) (any, error)) *cobra.Command {
	return &cobra.Command{
		Use: use, Short: short, Args: exactArgs(1, message),
		RunE: func(command *cobra.Command, args []string) error {
			value, err := operation(command, args[0])
			if err != nil {
				return err
			}
			return a.output(value)
		},
	}
}

func addPageFlags(command *cobra.Command, offset, limit *int, defaultLimit int) {
	command.Flags().IntVar(offset, "offset", 0, "zero-based result offset")
	command.Flags().IntVar(limit, "limit", defaultLimit, "page size between 1 and 500")
}

func validatePage(offset, limit int) error {
	if offset < 0 {
		return usage("--offset must be non-negative")
	}
	if limit < 1 || limit > 500 {
		return usage("--limit must be between 1 and 500")
	}
	return nil
}

func addAccountFields(command *cobra.Command, name, alias, description, avatar *string, serviceType *int) {
	command.Flags().StringVar(name, "name", "", "account display name")
	command.Flags().StringVar(alias, "alias", "", "account alias")
	command.Flags().StringVar(description, "description", "", "account description")
	command.Flags().StringVar(avatar, "avatar-url", "", "account avatar URL")
	command.Flags().IntVar(serviceType, "service-type", 0, "numeric WeChat service type")
}

func decodeJSONInput(path string, stdin io.Reader, target any) error {
	var file *os.File
	var err error
	if path == "" || path == "-" {
		if stdin == nil {
			return errors.New("stdin is unavailable")
		}
		decoder := json.NewDecoder(stdin)
		decoder.DisallowUnknownFields()
		return decoder.Decode(target)
	}
	file, err = os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func writePrivateJSONFile(path string, value any) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func parseOptionalTime(value string) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, errors.New("must be RFC3339 or YYYY-MM-DD")
}

func parseOptionalBool(value string) (*bool, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return nil, errors.New("must be true or false")
	}
	return &parsed, nil
}

func parseInts(value string) ([]int, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	items := []int{}
	for _, part := range strings.Split(value, ",") {
		parsed, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			return nil, fmt.Errorf("%q is not an integer", part)
		}
		items = append(items, parsed)
	}
	return items, nil
}

func parseSorts(value string) ([]domain.ArticleSort, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	items := []domain.ArticleSort{}
	for _, part := range strings.Split(value, ",") {
		field, direction, found := strings.Cut(strings.TrimSpace(part), ":")
		if !found || strings.TrimSpace(field) == "" {
			return nil, fmt.Errorf("%q must be field:asc or field:desc", part)
		}
		direction = strings.ToLower(strings.TrimSpace(direction))
		if direction != string(domain.SortAscending) && direction != string(domain.SortDescending) {
			return nil, fmt.Errorf("%q has an unsupported direction", part)
		}
		items = append(items, domain.ArticleSort{Field: strings.TrimSpace(field), Direction: domain.SortDirection(direction)})
	}
	return items, nil
}
