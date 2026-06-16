package meshio

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

const partWithMesh = `<?xml version="1.0"?>
<model unit="millimeter" xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02">
 <resources>
  <object id="1" type="model">
   <mesh>
    <vertices>
     <vertex x="0" y="0" z="0"/><vertex x="1" y="0" z="0"/><vertex x="0" y="1" z="0"/>
    </vertices>
    <triangles><triangle v1="0" v2="1" v3="2"/></triangles>
   </mesh>
  </object>
 </resources>
 <build><item objectid="1" transform="1 0 0 0 1 0 0 0 1 5 0 0"/></build>
</model>`

const partWithComponents = `<?xml version="1.0"?>
<model unit="millimeter" xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02"
       xmlns:p="http://schemas.microsoft.com/3dmanufacturing/production/2015/06">
 <resources>
  <object id="2" type="model">
   <components>
    <component p:path="/3D/Objects/sub.model" objectid="1" transform="1 0 0 0 1 0 0 0 1 0 0 0"/>
   </components>
  </object>
 </resources>
 <build><item objectid="2" transform="1 0 0 0 1 0 0 0 1 0 0 0"/></build>
</model>`

func TestParseModelPart_Mesh(t *testing.T) {
	p, err := parseModelPart(strings.NewReader(partWithMesh))
	if err != nil {
		t.Fatal(err)
	}
	obj := p.objects["1"]
	if obj == nil || obj.mesh == nil {
		t.Fatalf("object 1 mesh missing: %+v", p.objects)
	}
	if len(obj.mesh.vertices) != 9 || len(obj.mesh.indices) != 3 {
		t.Errorf("mesh verts=%d idx=%d want 9,3", len(obj.mesh.vertices), len(obj.mesh.indices))
	}
	if len(p.buildItems) != 1 || p.buildItems[0].objectID != "1" {
		t.Fatalf("build items = %+v", p.buildItems)
	}
	tx, _, _ := p.buildItems[0].transform.apply(0, 0, 0)
	if !approx(tx, 5) {
		t.Errorf("build item transform x = %v want 5", tx)
	}
}

func TestParseModelPart_Components(t *testing.T) {
	p, err := parseModelPart(strings.NewReader(partWithComponents))
	if err != nil {
		t.Fatal(err)
	}
	obj := p.objects["2"]
	if obj == nil || len(obj.components) != 1 {
		t.Fatalf("object 2 components missing: %+v", p.objects)
	}
	c := obj.components[0]
	if c.objectID != "1" || c.path != "/3D/Objects/sub.model" {
		t.Errorf("component = %+v", c)
	}
}

const relsXML = `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
 <Relationship Target="/3D/3dmodel.model" Id="rel0" Type="http://schemas.microsoft.com/3dmanufacturing/2013/01/3dmodel"/>
 <Relationship Target="/Metadata/thumbnail.png" Id="rel1" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/thumbnail"/>
</Relationships>`

func TestRootModelFromRels(t *testing.T) {
	got := rootModelFromRels(relsXML)
	if got != "3D/3dmodel.model" {
		t.Errorf("rootModelFromRels = %q want 3D/3dmodel.model", got)
	}
	if rootModelFromRels("<Relationships/>") != "" {
		t.Error("want empty for no 3dmodel relationship")
	}
}

func make3MF(parts map[string]string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range parts {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte(content))
	}
	_ = zw.Close()
	return buf.Bytes()
}

const ctXML = `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="model" ContentType="application/vnd.ms-package.3dmanufacturing-3dmodel+xml"/><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/></Types>`

const relsRoot = `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Target="/3D/3dmodel.model" Id="r0" Type="http://schemas.microsoft.com/3dmanufacturing/2013/01/3dmodel"/></Relationships>`

const rootComponents = `<?xml version="1.0"?>
<model unit="millimeter" xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02" xmlns:p="http://schemas.microsoft.com/3dmanufacturing/production/2015/06">
 <resources>
  <object id="2" type="model"><components>
   <component p:path="/3D/Objects/sub.model" objectid="1" transform="1 0 0 0 1 0 0 0 1 0 0 0"/>
  </components></object>
 </resources>
 <build><item objectid="2" transform="1 0 0 0 1 0 0 0 1 5 0 0"/></build>
</model>`

const subMesh = `<?xml version="1.0"?>
<model unit="millimeter" xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02">
 <resources>
  <object id="1" type="model"><mesh>
   <vertices><vertex x="0" y="0" z="0"/><vertex x="1" y="0" z="0"/><vertex x="0" y="1" z="0"/></vertices>
   <triangles><triangle v1="0" v2="1" v3="2"/></triangles>
  </mesh></object>
 </resources>
</model>`

func TestDecode3MF_ProductionExtension(t *testing.T) {
	data := make3MF(map[string]string{
		"[Content_Types].xml":  ctXML,
		"_rels/.rels":          relsRoot,
		"3D/3dmodel.model":     rootComponents,
		"3D/Objects/sub.model": subMesh,
	})
	m, err := Decode3MF(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Decode3MF: %v", err)
	}
	if len(m.Vertices) != 9 || len(m.Indices) != 3 {
		t.Fatalf("verts=%d idx=%d want 9,3", len(m.Vertices), len(m.Indices))
	}
	if !approx(m.Vertices[0], 5) || !approx(m.Vertices[1], 0) || !approx(m.Vertices[2], 0) {
		t.Errorf("vertex0 = %v,%v,%v want 5,0,0 (build item +5 x)", m.Vertices[0], m.Vertices[1], m.Vertices[2])
	}
}

func TestParseModelPart_BadTransformErrors(t *testing.T) {
	bad := `<?xml version="1.0"?><model xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02"><resources><object id="1" type="model"><components><component objectid="2" transform="1 2 3"/></components></object></resources></model>`
	if _, err := parseModelPart(strings.NewReader(bad)); err == nil {
		t.Error("want error for malformed component transform, got nil")
	}
}

func TestDecode3MF_CycleGuard(t *testing.T) {
	root := `<?xml version="1.0"?><model xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02"><resources><object id="1" type="model"><components><component objectid="1" transform="1 0 0 0 1 0 0 0 1 0 0 0"/></components></object></resources><build><item objectid="1" transform="1 0 0 0 1 0 0 0 1 0 0 0"/></build></model>`
	data := make3MF(map[string]string{"[Content_Types].xml": ctXML, "_rels/.rels": relsRoot, "3D/3dmodel.model": root})
	if _, err := Decode3MF(bytes.NewReader(data)); err == nil {
		t.Error("want error for component cycle, got nil")
	}
}
