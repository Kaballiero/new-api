// gen-admin-openapi reads the existing docs/openapi/api.json admin spec,
// parses Go struct types from model/* via AST, and rewrites every operation's
// responses["200"] with a typed ApiResponseOf<X> wrapper based on the manifest
// in this package. Untouched: top-level info/tags/security/servers, per-op
// summary/description/tags/parameters/security.
//
// requestBody policy:
//   - placeholder bodies from the legacy spec template are wiped (clearPlaceholderBodies)
//   - controller AST analysis is authoritative (enrichFromHandlers)
//   - manifest.Body provides a fallback $ref when AST yields nothing
//
// Pass -check to validate without writing the file (CI hook).
//
// Usage:
//   go run ./cmd/gen-admin-openapi/         (regenerate + write)
//   go run ./cmd/gen-admin-openapi/ -check  (validate only, exit 1 on errors)
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
)

var checkOnly = flag.Bool("check", false, "validate without writing api.json (CI mode)")

func main() {
	flag.Parse()
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	if err := parseModels("./model"); err != nil {
		return fmt.Errorf("parse models: %w", err)
	}
	// Pull request DTO struct types from auxiliary packages so cross-package
	// types referenced by handlers (e.g. dto.UpstreamRequest, ionet.DeploymentRequest)
	// also resolve to schemas.
	for _, dir := range []string{"./dto", "./pkg/ionet"} {
		if err := parseModels(dir); err != nil {
			return fmt.Errorf("parse %s: %w", dir, err)
		}
	}
	// Index package-level vars/consts BEFORE controller analysis so handler
	// AST walks can resolve SelectorExpr like `common.Version` to typed values.
	for _, dir := range []string{
		"./common",
		"./setting",
		"./setting/system_setting",
		"./setting/operation_setting",
		"./setting/console_setting",
		"./setting/ratio_setting",
		"./setting/billing_setting",
		"./setting/performance_setting",
		"./constant",
		"./service",
	} {
		scanPackageVars(dir)
	}
	if err := parseControllers("./controller"); err != nil {
		return fmt.Errorf("parse controllers: %w", err)
	}
	if err := parseRoutes("./router"); err != nil {
		return fmt.Errorf("parse routes: %w", err)
	}
	dedupeRoutes()

	rawSpec, err := os.ReadFile("./docs/openapi/api.json")
	if err != nil {
		return fmt.Errorf("read spec: %w", err)
	}
	var spec map[string]interface{}
	if err := json.Unmarshal(rawSpec, &spec); err != nil {
		return fmt.Errorf("parse spec: %w", err)
	}

	paths, _ := spec["paths"].(map[string]interface{})
	if paths == nil {
		return fmt.Errorf("spec has no paths")
	}

	// Apply spec mutations first — they populate referencedTypes via manifest
	// + handler enrichment. buildSchemas reads referencedTypes to emit only
	// the types actually used (plus transitive deps).
	//
	// Order matters:
	//   1. removeFakePaths        — drop paths that don't exist in the router
	//   2. clearPlaceholderBodies — wipe legacy template bodies so step 4 wins
	//   3. applyManifest          — set responses + register manifest body refs
	//   4. enrichFromHandlers     — AST-derived bodies and responses (authoritative)
	//   5. applyManifestBodies    — manifest-declared body fallback for endpoints
	//                               where AST analysis didn't yield a schema
	//   6. defaultUntypedResponses — ensure every operation has responses["200"]
	removeFakePaths(paths)
	clearPlaceholderBodies(paths)
	applyManifest(paths)
	enrichFromHandlers(paths)
	applyManifestBodies(paths)
	defaultUntypedResponses(paths)

	schemas := buildSchemas()

	components, _ := spec["components"].(map[string]interface{})
	if components == nil {
		components = map[string]interface{}{}
		spec["components"] = components
	}
	existingSchemas, _ := components["schemas"].(map[string]interface{})
	if existingSchemas == nil {
		existingSchemas = map[string]interface{}{}
	}
	for name, sch := range schemas {
		existingSchemas[name] = sch
	}
	components["schemas"] = existingSchemas

	// Sweep unreferenced schemas. The generator merges new schemas into the
	// existing map without removing stale entries — without this pass, schemas
	// from previous manifest versions persist as dead code in the spec and
	// surface in regenerated SDKs.
	swept := sweepUnreferencedSchemas(spec, existingSchemas)
	if swept > 0 {
		fmt.Printf("    swept:      %d unreferenced schema(s)\n", swept)
	}

	out, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	if !*checkOnly {
		if err := os.WriteFile("./docs/openapi/api.json", out, 0644); err != nil {
			return err
		}
		fmt.Println("ok: docs/openapi/api.json updated")
	} else {
		fmt.Println("check: skipping write (--check mode)")
	}
	fmt.Printf("    schemas:    %d\n", len(existingSchemas))
	fmt.Printf("    paths:      %d\n", len(paths))
	fmt.Printf("    routes:     %d\n", len(routes))
	// Surface dangling refs — useful for catching name drift after refactors.
	specJSON, _ := json.Marshal(spec)
	missing := []string{}
	for ref := range collectRefs(string(specJSON)) {
		if _, ok := existingSchemas[ref]; !ok {
			missing = append(missing, ref)
		}
	}
	if len(missing) > 0 {
		fmt.Printf("    WARN: %d dangling $refs:\n", len(missing))
		for _, m := range missing {
			fmt.Println("     -", m)
		}
	}

	// Run schema validation. Errors fail the build; warnings are surfaced.
	issues, _ := validateSpec(paths, components)
	errCount := printIssues(issues)
	if errCount > 0 {
		return fmt.Errorf("spec validation failed: %d errors", errCount)
	}
	return nil
}

// sweepUnreferencedSchemas removes components.schemas entries that are not
// reachable from paths via $ref, including transitive dependencies. Returns
// the count of removed schemas.
//
// Roots: every $ref appearing in spec.paths.
// Reachable: roots ∪ all $refs found inside reachable schemas (BFS).
// Removed: schemas not in reachable set.
func sweepUnreferencedSchemas(spec map[string]interface{}, schemas map[string]interface{}) int {
	paths, _ := spec["paths"].(map[string]interface{})
	if paths == nil {
		return 0
	}

	pathsJSON, _ := json.Marshal(paths)
	reachable := map[string]bool{}
	queue := []string{}
	for ref := range collectRefs(string(pathsJSON)) {
		if _, ok := schemas[ref]; ok && !reachable[ref] {
			reachable[ref] = true
			queue = append(queue, ref)
		}
	}

	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		schemaJSON, err := json.Marshal(schemas[name])
		if err != nil {
			continue
		}
		for ref := range collectRefs(string(schemaJSON)) {
			if _, ok := schemas[ref]; ok && !reachable[ref] {
				reachable[ref] = true
				queue = append(queue, ref)
			}
		}
	}

	removed := 0
	for name := range schemas {
		if !reachable[name] {
			delete(schemas, name)
			removed++
		}
	}
	return removed
}

func collectRefs(text string) map[string]bool {
	out := map[string]bool{}
	const pre = `"$ref":"#/components/schemas/`
	i := 0
	for {
		j := indexFrom(text, pre, i)
		if j < 0 {
			return out
		}
		j += len(pre)
		k := indexFrom(text, `"`, j)
		if k < 0 {
			return out
		}
		out[text[j:k]] = true
		i = k
	}
}

func indexFrom(s, sub string, from int) int {
	if from >= len(s) {
		return -1
	}
	idx := 0
	for idx < len(s)-from {
		if from+idx+len(sub) > len(s) {
			return -1
		}
		if s[from+idx:from+idx+len(sub)] == sub {
			return from + idx
		}
		idx++
	}
	return -1
}
