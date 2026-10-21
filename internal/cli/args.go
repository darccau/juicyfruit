package cli

import (
	"fmt"
	"strconv"
	"strings"
)

type FlagType int

const (
	StringFlag FlagType = iota
	BoolFlag
	MultiFlag
)

type Spec struct {
	Name      string
	Alias     string
	Type      FlagType
	Default   string
	Allowed   []string
	Negatable bool
	Mandatory bool
	Hidden    bool
	Help      string
	ValueHelp string
}

type Values struct {
	positionals []string
	strings     map[string]string
	lists       map[string][]string
	bools       map[string]bool
	parsed      map[string]bool
}

func (v *Values) Positionals() []string { return v.positionals }
func (v *Values) String(name string) string {
	if value, ok := v.strings[name]; ok {
		return value
	}
	return ""
}
func (v *Values) StringPtr(name string) *string {
	value, ok := v.strings[name]
	if !ok || !v.parsed[name] {
		return nil
	}
	return &value
}
func (v *Values) List(name string) []string {
	if values, ok := v.lists[name]; ok {
		return values
	}
	return []string{}
}
func (v *Values) Bool(name string) bool   { return v.bools[name] }
func (v *Values) Parsed(name string) bool { return v.parsed[name] }
func (v *Values) Int(name string) (int, error) {
	value := v.String(name)
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("Invalid --%s value: %s", name, value)
	}
	return parsed, nil
}
func (v *Values) Float(name string, fallback float64) (float64, error) {
	value := v.String(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("Invalid --%s value: %s", name, value)
	}
	return parsed, nil
}

type Parser struct {
	specs  []*Spec
	byName map[string]*Spec
	alias  map[string]*Spec
}

func NewParser(specs ...Spec) *Parser {
	p := &Parser{byName: map[string]*Spec{}, alias: map[string]*Spec{}}
	for index := range specs {
		copy := specs[index]
		p.specs = append(p.specs, &copy)
		p.byName[copy.Name] = &copy
		if copy.Alias != "" {
			p.alias[copy.Alias] = &copy
		}
	}
	return p
}

func (p *Parser) Spec(name string) *Spec { return p.byName[name] }

func (p *Parser) Parse(args []string) (*Values, error) {
	values := &Values{
		strings: map[string]string{}, lists: map[string][]string{},
		bools: map[string]bool{}, parsed: map[string]bool{},
	}
	for _, spec := range p.specs {
		switch spec.Type {
		case StringFlag:
			if spec.Default != "" {
				values.strings[spec.Name] = spec.Default
			}
		case BoolFlag:
			values.bools[spec.Name] = spec.Default == "true"
		}
	}
	terminated := false
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if terminated || arg == "-" || !strings.HasPrefix(arg, "-") {
			values.positionals = append(values.positionals, arg)
			continue
		}
		if arg == "--" {
			terminated = true
			continue
		}
		name := arg
		explicitValue := ""
		hasValue := false
		if strings.HasPrefix(arg, "--") {
			name = arg[2:]
			if cut := strings.IndexByte(name, '='); cut >= 0 {
				explicitValue = name[cut+1:]
				name = name[:cut]
				hasValue = true
			}
		} else {
			name = arg[1:]
			if cut := strings.IndexByte(name, '='); cut >= 0 {
				explicitValue = name[cut+1:]
				name = name[:cut]
				hasValue = true
			}
		}
		spec := p.byName[name]
		if spec == nil {
			spec = p.alias[name]
		}
		negative := false
		if spec == nil && strings.HasPrefix(name, "no-") {
			candidate := p.byName[name[3:]]
			if candidate != nil && candidate.Type == BoolFlag && candidate.Negatable {
				spec = candidate
				negative = true
			}
		}
		if spec == nil {
			return nil, fmt.Errorf("Could not find an option named \"%s\".", name)
		}
		if hasValue && spec.Type == BoolFlag && !negative {
			return nil, fmt.Errorf("Option --%s does not take a value.", spec.Name)
		}
		if spec.Type == BoolFlag {
			values.bools[spec.Name] = !negative
			values.parsed[spec.Name] = true
			continue
		}
		value := explicitValue
		if !hasValue {
			if index+1 >= len(args) {
				return nil, fmt.Errorf("Missing argument for --%s.", spec.Name)
			}
			index++
			value = args[index]
		}
		if len(spec.Allowed) > 0 {
			valid := false
			for _, allowed := range spec.Allowed {
				if allowed == value {
					valid = true
					break
				}
			}
			if !valid {
				return nil, fmt.Errorf("Invalid value \"%s\" for --%s. Allowed: %s.", value, spec.Name, strings.Join(spec.Allowed, ", "))
			}
		}
		values.parsed[spec.Name] = true
		if spec.Type == MultiFlag {
			values.lists[spec.Name] = append(values.lists[spec.Name], value)
		} else {
			values.strings[spec.Name] = value
		}
	}
	for _, spec := range p.specs {
		if spec.Mandatory && !values.parsed[spec.Name] {
			return nil, fmt.Errorf("Missing required option --%s.", spec.Name)
		}
	}
	return values, nil
}
