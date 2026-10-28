package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
)

// Preserve the published operation shape while resolving its relocated handler.
func parseAuditHandlers() error {
	file, err := parser.ParseFile(token.NewFileSet(), "internal/audit/admin_list_events/http.go", nil, parser.SkipObjectResolution)
	if err != nil {
		return err
	}
	for _, declaration := range file.Decls {
		fn, ok := declaration.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || fn.Name.Name != "Handle" {
			continue
		}
		info := &HandlerInfo{Name: "GetAuditLogs", QueryParams: analyzeHandler(fn).QueryParams}
		queryFile, err := parser.ParseFile(token.NewFileSet(), "internal/audit/transport/query.go", nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		for _, queryDeclaration := range queryFile.Decls {
			queryReader, ok := queryDeclaration.(*ast.FuncDecl)
			if !ok || queryReader.Name.Name != "ReadQuery" {
				continue
			}
			for _, param := range analyzeHandler(queryReader).QueryParams {
				info.QueryParams = appendUniqueParam(info.QueryParams, param)
			}
			handlers[info.Name] = info
			return nil
		}
		return fmt.Errorf("missing audit query reader")
	}
	return fmt.Errorf("missing relocated audit handler")
}
