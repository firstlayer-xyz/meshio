package meshio

import (
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
