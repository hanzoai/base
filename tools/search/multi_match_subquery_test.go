package search_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/hanzoai/orm/query"
	"github.com/hanzoai/base/tools/search"
)

func TestMultiMatchSubqueryBuild(t *testing.T) {
	// create a dummy db
	sqlDB, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	db := query.NewFromDB(sqlDB, "sqlite")

	mm := search.MultiMatchSubquery{
		TargetTableAlias: "test_TargetTableAlias",
		FromTableName:    "test_FromTableName",
		FromTableAlias:   "test_FromTableAlias",
		ValueIdentifier:  "({:mm},{:external})",
		Joins: []*search.Join{
			{TableName: "join_table1", TableAlias: "join_alias1"},
			{TableName: "join_table2", TableAlias: "join_alias2", On: query.NewExp("123={:join}", query.Params{"join": "test_join"})},
		},
		Params: query.Params{"mm": "test_mm"},
	}

	params := query.Params{"external": "test_external"}

	result := mm.Build(db, params)

	expectedResult := "SELECT ({:mm},{:external}) as [[multiMatchValue]] FROM `test_FromTableName` `test_FromTableAlias` LEFT JOIN `join_table1` `join_alias1` LEFT JOIN `join_table2` `join_alias2` ON 123={:join} WHERE `test_FromTableAlias`.`id` = `test_TargetTableAlias`.`id`"
	if expectedResult != result {
		t.Fatalf("Expected build result\n%v\ngot\n%v", expectedResult, result)
	}

	// the params from all expressions should be merged in the root
	rawParams, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}

	expectedParams := []byte(`{"external":"test_external","join":"test_join","mm":"test_mm"}`)
	if !bytes.Equal(rawParams, expectedParams) {
		t.Fatalf("Expected final params\n%s\ngot\n%s", expectedParams, rawParams)
	}
}
