package meshio

import (
	"archive/zip"
	"encoding/xml"
	"io"
	"strings"
)

// rootModelFromRels returns the package-relative path of the 3MF root model part
// (the Target of the Relationship whose Type is the 3dmodel relationship), or ""
// if none is present. The leading "/" is stripped to match zip entry names.
func rootModelFromRels(relsXML string) string {
	dec := xml.NewDecoder(strings.NewReader(relsXML))
	for {
		tok, err := dec.Token()
		if err != nil {
			return ""
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "Relationship" {
			continue
		}
		var target, typ string
		for _, a := range se.Attr {
			switch a.Name.Local {
			case "Target":
				target = a.Value
			case "Type":
				typ = a.Value
			}
		}
		if strings.HasSuffix(typ, "/3dmodel") {
			return strings.TrimPrefix(target, "/")
		}
	}
}

// findRootModelPart returns the root model part name. It prefers the OPC
// _rels/.rels 3dmodel relationship; falls back to "3D/3dmodel.model", then to
// the sole .model part if there is exactly one.
func findRootModelPart(zr *zip.Reader) string {
	for _, f := range zr.File {
		if f.Name == "_rels/.rels" {
			if rc, err := f.Open(); err == nil {
				b, _ := io.ReadAll(rc)
				rc.Close()
				if root := rootModelFromRels(string(b)); root != "" {
					return root
				}
			}
		}
	}
	var models []string
	for _, f := range zr.File {
		if f.Name == "3D/3dmodel.model" {
			return f.Name
		}
		if strings.HasSuffix(f.Name, ".model") {
			models = append(models, f.Name)
		}
	}
	if len(models) == 1 {
		return models[0]
	}
	return ""
}
