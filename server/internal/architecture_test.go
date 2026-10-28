package internal

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The rules are described in AGENTS.md next to this file. knownViolations
// records the debt that existed when a rule was introduced; it may only shrink.

const modulePath = "github.com/QuantumNous/new-api"

var infrastructurePackages = []string{
	"github.com/gin-gonic/gin",
	"gorm.io/",
	"github.com/go-redis/redis",
	"github.com/redis/go-redis",
}

func TestInternalArchitecture(t *testing.T) {
	serverDir, err := filepath.Abs("..")
	require.NoError(t, err)
	deps, packageImports := listPackageDeps(t, serverDir)
	// The key identifies a violation; the value explains it in failure messages.
	violations := map[string]string{}
	add := func(rule, location, detail string) {
		violations[rule+": "+location] = detail
	}
	addImport := func(rule, location, imported string) {
		violations[rule+": "+location+" -> "+imported] = imported
	}

	err = filepath.WalkDir(serverDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		// go list runs in a subprocess, so its inputs do not reach the test cache key.
		// Stat every Go file so that any change in the module invalidates a cached pass.
		if _, err := os.Stat(path); err != nil {
			return err
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(serverDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		parts := strings.Split(rel, "/")
		leaf := slices.Contains([]string{"constant", "common", "dto", "types", "relaykit"}, parts[0])
		if !leaf && parts[0] != "internal" && parts[0] != "model" && parts[0] != "service" {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		imports := make([]string, 0, len(file.Imports))
		for _, spec := range file.Imports {
			value, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			imports = append(imports, value)
		}

		if leaf {
			for _, imported := range imports {
				if strings.HasPrefix(imported, modulePath+"/internal/") {
					addImport("leaf-imports-internal", rel, imported)
				}
			}
			return nil
		}

		if parts[0] == "model" && isRecordAliasFile(file) {
			return nil
		}
		if parts[0] == "model" || parts[0] == "service" {
			for _, imported := range imports {
				if strings.HasPrefix(imported, modulePath+"/internal/") {
					addImport("legacy-imports-internal", rel, imported)
				}
			}
			return nil
		}
		if len(parts) < 3 || parts[1] == "app" || parts[1] == "adapter" || parts[1] == "contracttest" {
			return nil
		}
		module, name := parts[1], parts[len(parts)-1]
		if len(parts) == 3 {
			add("module-root-file", rel, module)
		}
		transport := len(parts) > 3 && parts[2] == "transport" || name == "http.go"
		scenarioLogic := len(parts) > 3 && !slices.Contains([]string{"domain", "dto", "transport"}, parts[2]) && name != "http.go" && name != "dto.go"
		if module == "pricing" && !transport {
			forbidden := pricingConstantAliasLeafViolations(imports, packageImports)
			if len(forbidden) > 0 {
				add("pricing-constant-alias-leaf", rel, strings.Join(forbidden, ", "))
			}
		}
		reached := map[string]bool{}
		for _, imported := range imports {
			local, isLocal := strings.CutPrefix(imported, modulePath+"/")
			if scenarioLogic && (isInfrastructure(imported) || isLocal && (local == "model" || local == "service" || strings.HasPrefix(local, "internal/adapter"))) {
				addImport("usecase-imports", rel, imported)
			}
			if scenarioLogic {
				for _, dep := range append([]string{imported}, deps[imported]...) {
					if isInfrastructure(dep) {
						reached[infrastructureRoot(dep)] = true
					}
				}
			}
			if transport && !transportImportAllowed(module, imported) {
				addImport("transport-imports", rel, imported)
			}
			if isLocal && (local == "relay" || strings.HasPrefix(local, "relay/") || local == "relaykit" || strings.HasPrefix(local, "relaykit/")) {
				addImport("business-imports-gateway", rel, imported)
			}
			if other, ok := strings.CutPrefix(local, "internal/"); isLocal && ok {
				otherParts := strings.Split(other, "/")
				if otherParts[0] != module && otherParts[0] != "adapter" && otherParts[0] != "app" && (len(otherParts) < 2 || otherParts[1] != "domain") {
					addImport("foreign-module-import", rel, imported)
				}
			}
		}
		if len(reached) > 0 {
			add("usecase-transitive", rel, joinedKeys(reached))
		}
		return nil
	})
	require.NoError(t, err)

	for pkg, packageDeps := range deps {
		local, ok := strings.CutPrefix(pkg, modulePath+"/internal/")
		if !ok || !strings.Contains("/"+local+"/", "/domain/") || strings.HasPrefix(local, "adapter/") || strings.HasPrefix(local, "app/") {
			continue
		}
		reached := map[string]bool{}
		for _, dep := range packageDeps {
			if isInfrastructure(dep) {
				reached[infrastructureRoot(dep)] = true
			}
		}
		if len(reached) > 0 {
			add("domain-transitive", "internal/"+local, joinedKeys(reached))
		}
	}

	if os.Getenv("ARCH_TEST_PRINT") != "" {
		found := make([]string, 0, len(violations))
		for violation := range violations {
			found = append(found, violation)
		}
		slices.Sort(found)
		for _, violation := range found {
			fmt.Printf("\t%q,\n", violation)
		}
	}

	known := map[string]bool{}
	for _, violation := range knownViolations {
		assert.False(t, known[violation], "duplicate entry in knownViolations: %s", violation)
		known[violation] = true
	}
	for violation, detail := range violations {
		assert.True(t, known[violation], "new architecture violation (see internal/AGENTS.md): %s (%s)", violation, detail)
	}
	for violation := range known {
		_, exists := violations[violation]
		assert.True(t, exists, "fixed violation is still listed in knownViolations, remove it: %s", violation)
	}
}

func listPackageDeps(t *testing.T, serverDir string) (map[string][]string, map[string][]string) {
	command := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}} {{join .Deps \" \"}}|{{join .Imports \" \"}}", "./...")
	command.Dir = serverDir
	output, err := command.Output()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		require.NoError(t, err, string(exitErr.Stderr))
	}
	require.NoError(t, err)
	deps := map[string][]string{}
	imports := map[string][]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(output)), "\n") {
		dependencies, direct, _ := strings.Cut(line, "|")
		fields := strings.Fields(dependencies)
		if len(fields) > 0 {
			deps[fields[0]] = fields[1:]
			imports[fields[0]] = strings.Fields(direct)
		}
	}
	return deps, imports
}

// transportImportAllowed is the allow-list for http.go and <module>/transport:
// stdlib, Gin, common, i18n, constant, the module's own domain/dto/transport and
// another module's domain.
func transportImportAllowed(module, imported string) bool {
	first, _, _ := strings.Cut(imported, "/")
	if !strings.Contains(first, ".") || strings.HasPrefix(imported, "github.com/gin-gonic/gin") {
		return true
	}
	local, isLocal := strings.CutPrefix(imported, modulePath+"/")
	if !isLocal {
		return false
	}
	if local == "common" || local == "i18n" || local == "constant" {
		return true
	}
	parts := strings.Split(local, "/")
	if len(parts) < 3 || parts[0] != "internal" {
		return false
	}
	if parts[1] == module {
		return slices.Contains([]string{"domain", "dto", "transport"}, parts[2])
	}
	return parts[1] != "adapter" && parts[1] != "app" && parts[2] == "domain"
}

func joinedKeys(set map[string]bool) string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return strings.Join(keys, ", ")
}

func isInfrastructure(importPath string) bool {
	return slices.ContainsFunc(infrastructurePackages, func(prefix string) bool { return strings.HasPrefix(importPath, prefix) })
}

func infrastructureRoot(importPath string) string {
	for _, prefix := range infrastructurePackages {
		if strings.HasPrefix(importPath, prefix) {
			return strings.TrimSuffix(prefix, "/")
		}
	}
	return importPath
}

// isRecordAliasFile permits only the technical compatibility surface explicitly
// allowed in model: aliases of one record package, with no executable declarations.
func isRecordAliasFile(file *ast.File) bool {
	if file.Name.Name != "model" || len(file.Imports) != 1 {
		return false
	}
	imported := file.Imports[0]
	path, err := strconv.Unquote(imported.Path.Value)
	if err != nil {
		return false
	}
	relative, ok := strings.CutPrefix(path, modulePath+"/internal/adapter/gorm/")
	parts := strings.Split(relative, "/")
	if !ok || len(parts) != 2 || parts[0] == "" || parts[0] == "." || parts[0] == ".." || parts[1] != "record" {
		return false
	}
	qualifier := "record"
	if imported.Name != nil {
		qualifier = imported.Name.Name
	}
	if qualifier == "_" || qualifier == "." {
		return false
	}
	alias := func(expression ast.Expr, name string) bool {
		selector, ok := expression.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != name {
			return false
		}
		pkg, ok := selector.X.(*ast.Ident)
		return ok && pkg.Name == qualifier
	}
	count := 0
	for _, declaration := range file.Decls {
		group, ok := declaration.(*ast.GenDecl)
		if !ok {
			return false
		}
		switch group.Tok {
		case token.IMPORT:
			// The sole import was validated above.
		case token.TYPE:
			for _, spec := range group.Specs {
				item, ok := spec.(*ast.TypeSpec)
				if !ok || !item.Assign.IsValid() || item.TypeParams != nil || !alias(item.Type, item.Name.Name) {
					return false
				}
				count++
			}
		case token.CONST:
			for _, spec := range group.Specs {
				item, ok := spec.(*ast.ValueSpec)
				if !ok || item.Type != nil || len(item.Names) != len(item.Values) {
					return false
				}
				for i, name := range item.Names {
					if !alias(item.Values[i], name.Name) {
						return false
					}
					count++
				}
			}
		default:
			return false
		}
	}
	return count > 0
}

func TestRecordAliasExceptionRejectsNonAliasDeclarations(t *testing.T) {
	const recordImport = `import "github.com/QuantumNous/new-api/internal/adapter/gorm/usage/record"`
	for _, tc := range []struct {
		name, body string
		allowed    bool
	}{
		{"type", "type Log = record.Log", true},
		{"constant", "const LogTypeConsume = record.LogTypeConsume", true},
		{"both", "type Log = record.Log; const LogTypeConsume = record.LogTypeConsume", true},
		{"defined_type", "type Log record.Log", false},
		{"function", "type Log = record.Log; func Run() {}", false},
		{"method", "type Log = record.Log; func (Log) Run() {}", false},
		{"variable", "type Log = record.Log; var Value = 1", false},
		{"init", "type Log = record.Log; func init() {}", false},
		{"literal_constant", "const LogTypeConsume = 2", false},
		{"expression_constant", "const LogTypeConsume = record.LogTypeConsume + 0", false},
		{"implicit_constant", "const (LogTypeConsume = record.LogTypeConsume; Other)", false},
		{"different_export", "type Log = record.Other", false},
		{"other_qualifier", "type Log = other.Log", false},
		{"extra_import", "import \"fmt\"; type Log = record.Log", false},
		{"import_only", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file, err := parser.ParseFile(token.NewFileSet(), "alias.go", "package model;"+recordImport+";"+tc.body, 0)
			require.NoError(t, err)
			assert.Equal(t, tc.allowed, isRecordAliasFile(file))
		})
	}
}

// pricingConstantAliasLeafViolations permits only the existing constant alias
// edges into the two exact relaykit leaves. It is not a gateway prefix exception.
func pricingConstantAliasLeafViolations(roots []string, imports map[string][]string) []string {
	type edge struct{ from, to string }
	pending := make([]edge, 0, len(roots))
	for _, root := range roots {
		pending = append(pending, edge{to: root})
	}
	seen := make(map[string]bool)
	forbidden := make(map[string]bool)
	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		local, isLocal := strings.CutPrefix(current.to, modulePath+"/")
		denied := false
		if isLocal {
			for _, name := range []string{"common", "pkg/billingexpr", "pkg/jsplugin", "pkg/catalogsync", "relay", "relaykit"} {
				if local == name || strings.HasPrefix(local, name+"/") {
					denied = true
					break
				}
			}
			switch current.to {
			case modulePath + "/relaykit/types":
				denied = current.from != modulePath+"/constant"
			case modulePath + "/relaykit/relayconvert/kitutil":
				denied = current.from != modulePath+"/relaykit/types"
			}
		}
		if denied {
			forbidden[current.from+" -> "+current.to] = true
			continue
		}
		if seen[current.to] {
			continue
		}
		seen[current.to] = true
		for _, next := range imports[current.to] {
			pending = append(pending, edge{from: current.to, to: next})
		}
	}
	var result []string
	for path := range forbidden {
		result = append(result, path)
	}
	slices.Sort(result)
	return result
}

func TestPricingConstantAliasLeafBoundary(t *testing.T) {
	leaf := modulePath + "/constant"
	types := modulePath + "/relaykit/types"
	codec := modulePath + "/relaykit/relayconvert/kitutil"
	for _, tc := range []struct {
		name      string
		roots     []string
		imports   map[string][]string
		forbidden []string
	}{
		{name: "existing alias chain", roots: []string{leaf}, imports: map[string][]string{leaf: {types}, types: {codec}}},
		{name: "direct type import", roots: []string{types}, forbidden: []string{" -> " + types}},
		{name: "alternate type owner", roots: []string{"helper"}, imports: map[string][]string{"helper": {types}}, forbidden: []string{"helper -> " + types}},
		{name: "direct codec import", roots: []string{codec}, forbidden: []string{" -> " + codec}},
		{name: "constant codec bypass", roots: []string{leaf}, imports: map[string][]string{leaf: {codec}}, forbidden: []string{leaf + " -> " + codec}},
		{name: "gateway dto behind constant", roots: []string{leaf}, imports: map[string][]string{leaf: {modulePath + "/relaykit/dto"}}, forbidden: []string{leaf + " -> " + modulePath + "/relaykit/dto"}},
		{name: "engine behind leaf", roots: []string{leaf}, imports: map[string][]string{leaf: {types}, types: {codec}, codec: {modulePath + "/pkg/billingexpr"}}, forbidden: []string{codec + " -> " + modulePath + "/pkg/billingexpr"}},
		{name: "common behind leaf", roots: []string{leaf}, imports: map[string][]string{leaf: {types}, types: {modulePath + "/common"}}, forbidden: []string{types + " -> " + modulePath + "/common"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.forbidden, pricingConstantAliasLeafViolations(tc.roots, tc.imports))
		})
	}
}

// TestLegacyArchitectureClosure keeps the completed legacy boundary closed.
func TestLegacyArchitectureClosure(t *testing.T) {
	require.Empty(t, knownViolations, "the completed architecture has no allowlist")
	serverDir, err := filepath.Abs("..")
	require.NoError(t, err)
	tables := strings.Fields("Ability AuditLog AuthFlow AuthzRole BatchItem BatchJob BatchLogEvent BatchLogReceipt BatchQuotaEvent CasbinRule CatalogDocument CatalogRevision CatalogSource Channel Checkin CustomOAuthProvider ExternalIdentityClaim Log LoginEncryptionKey Midjourney Model ModelCostFXRow Option PasskeyCredential PerfMetric PrefillGroup QuotaData Redemption Setup SubscriptionOrder SubscriptionPlan SubscriptionPreConsumeRecord SystemInstance SystemTask SystemTaskLock Task TaskPlugin Token TopUp TwoFA TwoFABackupCode User UserOAuthBinding UserSession UserSubscription Vendor")
	bootstrap := strings.Fields("audit_log.go batch_migration.go external_identity_claim_migration.go frontend_option_migration.go gorm_logger.go main.go migration_dialector.go option_primary_key_migration.go prefill_group_migration.go token_migration.go user_auth_migration.go")
	aliases := map[string]string{}
	consumers := map[string]bool{}
	err = filepath.WalkDir(serverDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, err := filepath.Rel(serverDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		assert.False(t, strings.HasPrefix(rel, "service/"), "service must contain fixtures only: %s", rel)
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		for _, group := range file.Comments {
			for _, comment := range group.List {
				assert.False(t, strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(comment.Text, "//")), "BRIDGE"), "remaining bridge in %s", rel)
			}
		}
		if !strings.HasPrefix(rel, "model/") {
			return nil
		}
		if slices.Contains(bootstrap, filepath.Base(path)) {
			ast.Inspect(file, func(node ast.Node) bool {
				if id, ok := node.(*ast.Ident); ok {
					consumers[id.Name] = true
				}
				return true
			})
			return nil
		}
		if !assert.True(t, isRecordAliasFile(file), "model only permits bootstrap/migrations or table aliases: %s", rel) {
			return nil
		}
		for _, decl := range file.Decls {
			group := decl.(*ast.GenDecl)
			if group.Tok == token.IMPORT {
				continue
			}
			if !assert.Equal(t, token.TYPE, group.Tok, "legacy constants are forbidden: %s", rel) {
				continue
			}
			for _, spec := range group.Specs {
				name := spec.(*ast.TypeSpec).Name.Name
				assert.Contains(t, tables, name, "non-table model alias in %s", rel)
				aliases[name] = rel
			}
		}
		return nil
	})
	require.NoError(t, err)
	for name, path := range aliases {
		assert.True(t, consumers[name], "model alias has no migration/bootstrap consumer: %s (%s)", name, path)
	}
}
