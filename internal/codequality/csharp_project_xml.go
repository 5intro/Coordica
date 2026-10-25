package codequality

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// Read a bounded static subset; never evaluate MSBuild, SDKs or imports. Unknown
// membership expressions fail closed instead of joining unrelated assemblies.
func parseCSharpProject(text string, p *csharpProject) error {
	d := xml.NewDecoder(strings.NewReader(text))
	root := false
	defaults := map[string]bool{"EnableDefaultItems": true, "EnableDefaultCompileItems": true}
	for {
		token, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		attrs := map[string]string{}
		for _, a := range start.Attr {
			attrs[a.Name.Local] = a.Value
		}
		if attrs["Condition"] != "" {
			return fmt.Errorf("conditional project membership requires native analysis")
		}
		if !root {
			if start.Name.Local != "Project" {
				return fmt.Errorf("expected Project root")
			}
			root = true
			p.defaultCompile = attrs["Sdk"] == "Microsoft.NET.Sdk" || attrs["Sdk"] == "Microsoft.NET.Sdk.Web" ||
				attrs["Sdk"] == "Microsoft.NET.Sdk.Razor"
			if attrs["Sdk"] != "" && !p.defaultCompile {
				return fmt.Errorf("custom SDK requires native analysis")
			}
		}
		switch start.Name.Local {
		case "Import", "Sdk", "Target":
			return fmt.Errorf("explicit project imports require native analysis")
		case "EnableDefaultItems", "EnableDefaultCompileItems":
			var value string
			if err := d.DecodeElement(&value, &start); err != nil {
				return err
			}
			value = strings.ToLower(strings.TrimSpace(value))
			if value != "true" && value != "false" {
				return fmt.Errorf("dynamic default compile policy")
			}
			defaults[start.Name.Local] = value == "true"
		case "DefaultItemExcludes", "DefaultItemExcludesInProjectFolder", "BaseIntermediateOutputPath", "BaseOutputPath":
			return fmt.Errorf("custom default membership requires native project analysis")
		case "Compile":
			item, err := csharpCompileItemFrom(p.root, attrs)
			if err != nil {
				return err
			}
			p.items = append(p.items, item)
		}
	}
	if !root {
		return fmt.Errorf("missing Project root")
	}
	p.defaultCompile = p.defaultCompile && defaults["EnableDefaultItems"] && defaults["EnableDefaultCompileItems"]
	return nil
}
