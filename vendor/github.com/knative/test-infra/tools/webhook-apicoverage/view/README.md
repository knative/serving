# View

This package contains types and helper methods that repos can use to display
API Coverage results.

[DisplayRules](rule.go) provides a mechanism for repos to define their own
display rules. DisplayHelper methods can use these rules to define how to
display results.

`GetJSONTypeDisplay()` is a utility method that can be used by repos to get a
JSON like textual display of API Coverage. This method takes an array of
[TypeCoverage](../coveragecalculator/coveragedata.go) and [DisplayRules](rule.go)
object and returns a string representing its coverage in the format:

```
Package: <PackageName>
Type: <TypeName>
{
    <FieldName> <Ignored>/<Coverage:TrueorFalse> [Values]
    ....
    ....
    ....
}
```

`GetCoverageValuesDisplay()` is a utility method that can be used by repos to produce
coverage values display. The method takes as input [CoverageValue](../coveragecalculator/calculator.go)
and produces a display in the format:

```
CoverageValues:

Total Fields:  <Number of total fields>
Covered Fields: <Number of fields covered>
Ignored Fields: <Number of fields ignored>
Coverage Percentage: <Percentage value of coverage>
```