package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"path/filepath"
)

type catalogHandlerSource struct {
	Name, Package, Body, Response, Inline string
	Status                                int
	Paged                                 bool
	Queries                               []QueryParam
	Errors                                []ErrorCode
}

// Preserve the published contract projection while reading real scenario DTOs and HTTP sources.
var catalogHandlerSources = []catalogHandlerSource{
	{Name: "GetAllModelsMeta", Package: "list_models", Body: "", Response: "", Inline: "", Status: 0, Paged: false, Queries: []QueryParam{}, Errors: []ErrorCode{}},
	{Name: "SearchModelsMeta", Package: "search_models", Body: "", Response: "", Inline: "", Status: 0, Paged: false, Queries: []QueryParam{{Name: "keyword", Type: "string"}, {Name: "vendor", Type: "string"}}, Errors: []ErrorCode{}},
	{Name: "GetModelMeta", Package: "get_model", Body: "", Response: "", Inline: "", Status: 0, Paged: false, Queries: []QueryParam{}, Errors: []ErrorCode{{Status: 400, Code: "invalid_params"}, {Status: 404, Code: "model_not_found"}, {Status: 500, Code: "internal_error"}}},
	{Name: "CreateModelMeta", Package: "create_model", Body: "Model", Response: "", Inline: "", Status: 201, Paged: false, Queries: []QueryParam{}, Errors: []ErrorCode{{Status: 400, Code: "invalid_params"}, {Status: 400, Code: "model_name_empty"}, {Status: 500, Code: "internal_error"}, {Status: 409, Code: "model_name_exists"}}},
	{Name: "UpdateModelMeta", Package: "update_model", Body: "Model", Response: "", Inline: "", Status: 0, Paged: false, Queries: []QueryParam{{Name: "status_only", Type: "string"}}, Errors: []ErrorCode{{Status: 400, Code: "invalid_params"}, {Status: 400, Code: "model_id_missing"}, {Status: 500, Code: "internal_error"}, {Status: 409, Code: "model_name_exists"}}},
	{Name: "DeleteModelMeta", Package: "delete_model", Body: "", Response: "ModelDeleteResult", Inline: "", Status: 0, Paged: false, Queries: []QueryParam{{Name: "remove_from_channels", Type: "string"}, {Name: "remove_pricing", Type: "string"}}, Errors: []ErrorCode{{Status: 400, Code: "invalid_params"}, {Status: 404, Code: "model_not_found"}, {Status: 500, Code: "internal_error"}}},
	{Name: "BatchDeleteModelMeta", Package: "delete_models_batch", Body: "", Response: "ModelDeleteResult", Inline: "Input", Status: 0, Paged: false, Queries: []QueryParam{}, Errors: []ErrorCode{}},
	{Name: "GetAllVendors", Package: "list_vendors", Body: "", Response: "", Inline: "", Status: 0, Paged: false, Queries: []QueryParam{}, Errors: []ErrorCode{}},
	{Name: "SearchVendors", Package: "search_vendors", Body: "", Response: "", Inline: "", Status: 0, Paged: true, Queries: []QueryParam{{Name: "p", Type: "integer"}, {Name: "page_size", Type: "integer"}, {Name: "keyword", Type: "string"}, {Name: "association", Type: "string"}}, Errors: []ErrorCode{{Status: 500, Code: "internal_error"}}},
	{Name: "GetVendorMeta", Package: "get_vendor", Body: "", Response: "Vendor", Inline: "", Status: 0, Paged: false, Queries: []QueryParam{}, Errors: []ErrorCode{{Status: 400, Code: "invalid_params"}, {Status: 404, Code: "vendor_not_found"}, {Status: 500, Code: "internal_error"}}},
	{Name: "CreateVendorMeta", Package: "create_vendor", Body: "Vendor", Response: "", Inline: "", Status: 201, Paged: false, Queries: []QueryParam{}, Errors: []ErrorCode{{Status: 400, Code: "invalid_params"}, {Status: 400, Code: "vendor_name_empty"}, {Status: 500, Code: "internal_error"}, {Status: 409, Code: "vendor_name_exists"}}},
	{Name: "UpdateVendorMeta", Package: "update_vendor", Body: "Vendor", Response: "", Inline: "", Status: 0, Paged: false, Queries: []QueryParam{}, Errors: []ErrorCode{{Status: 400, Code: "invalid_params"}, {Status: 400, Code: "vendor_id_missing"}, {Status: 400, Code: "vendor_name_empty"}, {Status: 500, Code: "internal_error"}, {Status: 409, Code: "vendor_name_exists"}}},
	{Name: "DeleteVendorMeta", Package: "delete_vendor", Body: "", Response: "", Inline: "", Status: 0, Paged: false, Queries: []QueryParam{}, Errors: []ErrorCode{{Status: 400, Code: "invalid_params"}}},
	{Name: "PreviewVendorOperation", Package: "preview_vendor_operation", Body: "VendorOperation", Response: "VendorOperationPreview", Inline: "", Status: 0, Paged: false, Queries: []QueryParam{}, Errors: []ErrorCode{}},
	{Name: "ApplyVendorOperation", Package: "apply_vendor_operation", Body: "VendorOperation", Response: "VendorOperationResult", Inline: "", Status: 0, Paged: false, Queries: []QueryParam{}, Errors: []ErrorCode{}},
	{Name: "GetMissingModels", Package: "list_missing_models", Body: "", Response: "", Inline: "", Status: 0, Paged: false, Queries: []QueryParam{}, Errors: []ErrorCode{{Status: 500, Code: "internal_error"}}},
	{Name: "SyncUpstreamPreview", Package: "preview_metadata_sync", Body: "", Response: "SyncUpstreamPreviewResponse", Inline: "", Status: 0, Paged: false, Queries: []QueryParam{{Name: "locale", Type: "string"}}, Errors: []ErrorCode{}},
	{Name: "SyncUpstreamModels", Package: "apply_metadata_sync", Body: "", Response: "MetadataSyncResult", Inline: "Input", Status: 0, Paged: false, Queries: []QueryParam{}, Errors: []ErrorCode{}},
}

func parseCatalogHandlers() error {
	preserveCatalogSchemaNames()
	for _, source := range catalogHandlerSources {
		savedTypes, savedAliases, savedFields := maps.Clone(modelTypes), maps.Clone(modelAliases), maps.Clone(fieldTypes)
		dir := filepath.Join("internal/catalog", source.Package)
		if err := parseModels(dir); err != nil {
			return err
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, "http.go"), nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Name.Name != "Handle" {
				continue
			}
			info := analyzeHandler(fn)
			info.Name = source.Name
			info.BodyType = source.Body
			info.BodySchema = nil
			if source.Inline != "" {
				info.BodySchema = channelInlineSchema(modelTypes[source.Inline])
			}
			info.RespType = source.Response
			info.RespSchema = nil
			info.RespIsList = false
			info.RespIsPaged = source.Paged
			info.RespStatus = source.Status
			info.QueryParams = source.Queries
			info.ErrorCodes = source.Errors
			handlers[source.Name] = info
		}
		if _, ok := handlers[source.Name]; !ok {
			return fmt.Errorf("missing relocated catalog handler: %s", source.Name)
		}
		modelTypes, modelAliases, fieldTypes = savedTypes, savedAliases, savedFields
	}
	return nil
}

func preserveCatalogSchemaNames() {
	rename := map[string]string{"MetadataSyncCandidate": "metadataSyncCandidate", "MetadataSyncField": "metadataSyncField"}
	for actual, public := range rename {
		if value := modelTypes[actual]; value != nil {
			modelTypes[public] = value
			delete(modelAliases, public)
		}
	}
	for _, value := range modelTypes {
		ast.Inspect(value, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				if name, exists := rename[id.Name]; exists {
					id.Name = name
				}
			}
			if selector, ok := n.(*ast.SelectorExpr); ok {
				if name, exists := rename[selector.Sel.Name]; exists {
					selector.Sel.Name = name
				}
			}
			return true
		})
	}
	modelAliases["ModelSquareState"] = "string"
}
