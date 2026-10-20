package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"path/filepath"
)

// Preserve public schema names while reading the actual business metadata and
// scenario DTO rather than maintaining a second set of catalog structs.
func parseAuthorizationHandlers() error {
	savedTypes, savedAliases, savedFields := maps.Clone(modelTypes), maps.Clone(modelAliases), maps.Clone(fieldTypes)
	if err := parseModels("internal/authorization/domain"); err != nil {
		return err
	}
	if err := parseModels("internal/authorization/get_permission_catalog"); err != nil {
		return err
	}
	names := map[string]string{"AuthzActionDefinition": "ActionDefinition", "AuthzResourceDefinition": "ResourceDefinition", "AuthzRoleDescriptor": "RoleDescriptor", "AuthzCatalogResponse": "Output"}
	for public, actual := range names {
		st := modelTypes[actual]
		for _, field := range st.Fields.List {
			if array, ok := field.Type.(*ast.ArrayType); ok {
				if id, ok := array.Elt.(*ast.Ident); ok {
					for schema, target := range names {
						if id.Name == target {
							id.Name = schema
							break
						}
					}
				}
			}
		}
		savedTypes[public] = st
		delete(savedAliases, public)
	}
	// Expand the actual named map in the role's grants, which the generic alias
	// parser otherwise treats as opaque.
	permissionFile, err := parser.ParseFile(token.NewFileSet(), "internal/authorization/domain/permission.go", nil, parser.SkipObjectResolution)
	if err != nil {
		return err
	}
	ast.Inspect(permissionFile, func(n ast.Node) bool {
		spec, ok := n.(*ast.TypeSpec)
		if ok && spec.Name.Name == "PermissionsMap" {
			for _, field := range savedTypes["AuthzRoleDescriptor"].Fields.List {
				if len(field.Names) > 0 && field.Names[0].Name == "Grants" {
					field.Type = spec.Type
				}
			}
		}
		return true
	})
	file, err := parser.ParseFile(token.NewFileSet(), filepath.Join("internal/authorization/get_permission_catalog", "http.go"), nil, parser.SkipObjectResolution)
	if err != nil {
		return err
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || fn.Name.Name != "Handle" {
			continue
		}
		info := analyzeHandler(fn)
		info.Name = "GetPermissionCatalog"
		handlers[info.Name] = info
	}
	modelTypes, modelAliases, fieldTypes = savedTypes, savedAliases, savedFields
	return nil
}
