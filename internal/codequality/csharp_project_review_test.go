package codequality

import (
	"errors"
	"strings"
	"testing"
)

const reviewSDKProject = `<Project Sdk="Microsoft.NET.Sdk"></Project>`

func TestCSharpExplicitCompileMembershipIsOrderedAndProjectScoped(t *testing.T) {
	const explicit = `<Project><ItemGroup><Compile Include="../shared/**/*.cs" Exclude="../shared/skip.cs"/>` +
		`<Compile Remove="../shared/reinclude.cs"/><Compile Include="../shared/reinclude.cs"/></ItemGroup></Project>`
	files := []File{{"one/app.csproj", explicit}, {"two/app.csproj", explicit},
		{"shared/first.cs", csharpProjectPart("First", 11)}, {"shared/reinclude.cs", csharpProjectPart("Again", 11)},
		{"shared/skip.cs", csharpProjectPart("Skip", 19)}}
	findings, err := Measure(files)
	if err != nil || len(findings) != 2 {
		t.Fatal("linked source membership", findings, err)
	}
	for _, f := range findings {
		if f.Rule != "class_methods" || f.Value != 22 {
			t.Fatal(f)
		}
	}
	files[0].Text = `<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup>` +
		`<EnableDefaultCompileItems>false</EnableDefaultCompileItems></PropertyGroup></Project>`
	files = append(files[:1], File{"one/a.cs", csharpProjectPart("A", 11)}, File{"one/b.cs", csharpProjectPart("B", 11)})
	findings, err = Measure(files)
	if err != nil || len(findings) != 0 {
		t.Fatal("disabled default includes", findings, err)
	}
}

func TestCSharpProjectDebtCannotTransferBetweenAssemblies(t *testing.T) {
	base := []File{{"one/app.csproj", reviewSDKProject}, {"two/app.csproj", reviewSDKProject},
		{"one/first.cs", csharpProjectPart("First", 12)}, {"one/second.cs", csharpProjectPart("Second", 11)}}
	cleanup := []File{base[0], base[1], {"one/second.cs", csharpProjectPart("Second", 21)}}
	findings, err := Regressions(base, cleanup)
	if err != nil || len(findings) != 0 {
		t.Fatal("same assembly cleanup lost allowance", findings, err)
	}
	cleanup[2].Path = "two/second.cs"
	findings, err = Regressions(base, cleanup)
	if err != nil || len(findings) != 1 || findings[0].Rule != "class_methods" {
		t.Fatal("cross-assembly debt transfer", findings, err)
	}
}

func TestCSharpUnknownProjectMembershipFailsClosedWithoutBaselineAllowance(t *testing.T) {
	for _, metadata := range []string{
		`<Project`, `<Project Sdk="Custom.Sdk"/>`, `<Project><Import Project="extra.props"/></Project>`,
		`<Project><ItemGroup Condition="something"><Compile Include="*.cs"/></ItemGroup></Project>`,
		`<Project><ItemGroup><Compile Include="$(Shared)/*.cs"/></ItemGroup></Project>`,
	} {
		base := []File{{"app.csproj", metadata}, {"work.cs", csharpProjectPart("Work", 21)}}
		_, err := Measure(base)
		var source *SourceError
		if !errors.As(err, &source) {
			t.Fatal("unknown project scope passed", metadata, err)
		}
		base[0].Text = reviewSDKProject
		findings, err := Regressions([]File{{"app.csproj", metadata}, base[1]}, base)
		if err != nil || len(findings) != 1 || findings[0].Rule != "class_methods" {
			t.Fatal("invalid baseline donated allowance", findings, err)
		}
	}
	files := []File{{"app.csproj", reviewSDKProject}, {"Directory.Build.props", "<Project/>"},
		{"work.cs", csharpProjectPart("Work", 21)}}
	if _, err := Measure(files); err == nil {
		t.Fatal("unresolved build imports silently passed")
	}
	files[1].Text = strings.Repeat(" ", MaxFileBytes+1)
	if _, err := Measure(files); err == nil {
		t.Fatal("metadata escaped bounded reader")
	}
}

func TestCSharpUnknownBaselineScopeRetainsOnlyPhysicalDebt(t *testing.T) {
	source := "// " + strings.Repeat("x", 161) + "\nclass Worker {}\n"
	base := []File{{"app.csproj", "<Project"}, {"work.cs", source}}
	candidate := []File{{"app.csproj", reviewSDKProject}, {"work.cs", source}}
	findings, err := Regressions(base, candidate)
	if err != nil || len(findings) != 0 {
		t.Fatal("measurable physical baseline debt was discarded", findings, err)
	}
}
