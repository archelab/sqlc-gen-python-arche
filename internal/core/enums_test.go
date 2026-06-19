package core

import (
	"strings"
	"testing"
)

// ModelType must be a distinct NewType, not a bare string. These assignments
// compile only if the const members are typed as ModelType.
func TestModelTypeIsNewType(t *testing.T) {
	var mt ModelType = ModelTypeDataclass
	members := []ModelType{
		ModelTypeDataclass,
		ModelTypeAttrs,
		ModelTypeMsgspec,
		ModelTypePydantic,
	}
	for _, m := range members {
		if _, ok := validModelTypes[m]; !ok {
			t.Fatalf("validModelTypes is missing member %q", m)
		}
	}
	if string(mt) != "dataclass" {
		t.Fatalf("ModelTypeDataclass = %q, want dataclass", mt)
	}
	if string(ModelTypePydantic) != "pydantic" {
		t.Fatalf("ModelTypePydantic = %q, want pydantic", ModelTypePydantic)
	}
}

func TestSQLAlchemyDriverMember(t *testing.T) {
	if string(SQLDriverSQLAlchemy) != "sqlalchemy" {
		t.Fatalf("SQLDriverSQLAlchemy = %q, want sqlalchemy", SQLDriverSQLAlchemy)
	}
	async, err := isDriverAsync(SQLDriverSQLAlchemy)
	if err != nil {
		t.Fatalf("isDriverAsync(sqlalchemy) errored: %v", err)
	}
	if !async {
		t.Fatalf("sqlalchemy must be async")
	}
	if got := driversEngine[SQLDriverSQLAlchemy]; got != "postgresql" {
		t.Fatalf("driversEngine[sqlalchemy] = %q, want postgresql", got)
	}
}

func TestIsModelTypeValidFailsLoud(t *testing.T) {
	if err := isModelTypeValid(ModelTypePydantic); err != nil {
		t.Fatalf("pydantic must be valid: %v", err)
	}
	err := isModelTypeValid(ModelType("bogus"))
	if err == nil {
		t.Fatal("isModelTypeValid must reject an unknown model type")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("error must name the offending value, got %q", err.Error())
	}
}

func TestIsDriverValidRejectsBadCombo(t *testing.T) {
	// sqlalchemy on the postgresql engine is fine.
	if err := isDriverValid(SQLDriverSQLAlchemy, "postgresql"); err != nil {
		t.Fatalf("sqlalchemy x postgresql must be valid: %v", err)
	}
	// sqlalchemy on a non-postgresql engine must fail loud, naming the driver.
	err := isDriverValid(SQLDriverSQLAlchemy, "sqlite")
	if err == nil {
		t.Fatal("sqlalchemy x sqlite must be rejected")
	}
	if !strings.Contains(err.Error(), "sqlalchemy") {
		t.Fatalf("error must name the driver, got %q", err.Error())
	}
	// an entirely unknown driver must fail loud too.
	if err := isDriverValid(SQLDriverType("nodriver"), "postgresql"); err == nil {
		t.Fatal("unknown driver must be rejected")
	}
}
