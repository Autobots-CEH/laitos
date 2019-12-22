package common

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/HouzuoGuo/laitos/lalog"
	"github.com/HouzuoGuo/laitos/misc"
	"github.com/HouzuoGuo/laitos/toolbox"
	"github.com/HouzuoGuo/laitos/toolbox/filter"
)

const (
	//ErrBadProcessorConfig is used as the prefix string in all errors returned by "IsSaneForInternet" function.
	ErrBadProcessorConfig = "bad configuration: "

	/*
		PrefixCommandPLT is the magic string to prefix command input, in order to navigate around among the output and
		temporarily alter execution timeout. PLT stands for "position, length, timeout".
	*/
	PrefixCommandPLT = ".plt"

	// TestCommandProcessorPIN is the PIN secret used in test command processor, as returned by GetTestCommandProcessor.
	TestCommandProcessorPIN = "verysecret"

	// MaxCmdPerSecHardLimit is the hard uppper limit of the approximate maximum number of commands a command processor will process in a second.
	MaxCmdPerSecHardLimit = 1000
)

// ErrBadPrefix is a command execution error triggered if the command does not contain a valid toolbox feature trigger.
var ErrBadPrefix = errors.New("bad prefix or feature is not configured")

// ErrBadPLT reminds user of the proper syntax to invoke PLT magic.
var ErrBadPLT = errors.New(PrefixCommandPLT + " P L T command")

// ErrRateLimitExceeded is a command execution error indicating that the internal command processing rate limit has been exceeded
var ErrRateLimitExceeded = errors.New("command processor internal rate limit has been exceeded")

// RegexCommandWithPLT parses PLT magic parameters position, length, and timeout, all of which are integers.
var RegexCommandWithPLT = regexp.MustCompile(`[^\d]*(\d+)[^\d]+(\d+)[^\d]*(\d+)(.*)`)

// Pre-configured environment and configuration for processing feature commands.
type CommandProcessor struct {
	Features       *toolbox.FeatureSet    // Features is the aggregation of initialised toolbox feature routines.
	CommandFilters []filter.CommandFilter // CommandFilters are applied one by one to alter input command content and/or timeout.
	ResultFilters  []filter.ResultFilter  // ResultFilters are applied one by one to alter command execution result.

	/*
		MaxCmdPerSec is the approximate maximum number of commands allowed to be processed per second.
		The limit is a defensive measure against an attacker trying to guess the correct password by using visiting a daemon
		from large range of source IP addresses in an attempt to bypass daemon's own per-IP rate limit mechanism.
	*/
	MaxCmdPerSec int
	rateLimit    *misc.RateLimit
	// initOnce helps to initialise the command processor in preparation for processing command for the first time.
	initOnce sync.Once

	logger lalog.Logger
}

// initialiseOnce prepares the command processor for processing command for the first time.
func (proc *CommandProcessor) initialiseOnce() {
	proc.initOnce.Do(func() {
		// Reset the maximum command per second limit
		if proc.MaxCmdPerSec < 1 || proc.MaxCmdPerSec > MaxCmdPerSecHardLimit {
			proc.MaxCmdPerSec = MaxCmdPerSecHardLimit
		}
		// Initialise command rate limit
		if proc.rateLimit == nil {
			proc.rateLimit = &misc.RateLimit{
				UnitSecs: 1,
				MaxCount: proc.MaxCmdPerSec,
				Logger:   proc.logger,
			}
			proc.rateLimit.Initialise()
		}
	})
}

// SetLogger uses the input logger to prepare the command processor and its filters.
func (proc *CommandProcessor) SetLogger(logger lalog.Logger) {
	// The command processor itself as well as the filters are going to share the same logger
	proc.logger = logger
	for _, b := range proc.ResultFilters {
		b.SetLogger(logger)
	}
}

/*
IsEmpty returns true only if the command processor does not appear to have a meaningful configuration, which means:
- It does not have a PIN filter (the password protection).
- There are no filters configured at all.
*/
func (proc *CommandProcessor) IsEmpty() bool {
	if proc.CommandFilters == nil || len(proc.CommandFilters) == 0 {
		// An empty processor does not have any filter configuration
		return true
	}
	for _, cmdFilter := range proc.CommandFilters {
		// An empty processor does not have a PIN
		if pinFilter, ok := cmdFilter.(*filter.PINAndShortcuts); ok && pinFilter.PIN == "" {
			return true
		}
	}
	return false
}

/*
From the prospect of Internet-facing mail processor and Twilio hooks, check that parameters are within sane range.
Return a zero-length slice if everything looks OK.
*/
func (proc *CommandProcessor) IsSaneForInternet() (errs []error) {
	errs = make([]error, 0)
	// Check for nils too, just in case.
	if proc.Features == nil {
		errs = append(errs, errors.New(ErrBadProcessorConfig+"FeatureSet is not assigned"))
	} else {
		if len(proc.Features.LookupByTrigger) == 0 {
			errs = append(errs, errors.New(ErrBadProcessorConfig+"FeatureSet is not initialised or all features are lacking configuration"))
		}
	}
	if proc.CommandFilters == nil {
		errs = append(errs, errors.New(ErrBadProcessorConfig+"CommandFilters is not assigned"))
	} else {
		// Check whether PIN bridge is sanely configured
		seenPIN := false
		for _, cmdBridge := range proc.CommandFilters {
			if pin, yes := cmdBridge.(*filter.PINAndShortcuts); yes {
				if pin.PIN == "" && (pin.Shortcuts == nil || len(pin.Shortcuts) == 0) {
					errs = append(errs, errors.New(ErrBadProcessorConfig+"Defined in PINAndShortcuts there has to be password PIN, command shortcuts, or both."))
				}
				if pin.PIN != "" && len(pin.PIN) < 7 {
					errs = append(errs, errors.New(ErrBadProcessorConfig+"Password PIN must be at least 7 characters long"))
				}
				seenPIN = true
				break
			}
		}
		if !seenPIN {
			errs = append(errs, errors.New(ErrBadProcessorConfig+"\"PINAndShortcuts\" filter must be defined to set up password PIN protection or command shortcuts"))
		}
	}
	if proc.ResultFilters == nil {
		errs = append(errs, errors.New(ErrBadProcessorConfig+"ResultFilters is not assigned"))
	} else {
		// Check whether string linter is sanely configured
		seenLinter := false
		for _, resultBridge := range proc.ResultFilters {
			if linter, yes := resultBridge.(*filter.LintText); yes {
				if linter.MaxLength < 35 || linter.MaxLength > 4096 {
					errs = append(errs, errors.New(ErrBadProcessorConfig+"Maximum output length for LintText must be within [35, 4096]"))
				}
				seenLinter = true
				break
			}
		}
		if !seenLinter {
			errs = append(errs, errors.New(ErrBadProcessorConfig+"\"LintText\" filter must be defined to restrict command output length"))
		}
	}
	return
}

/*
Process applies filters to the command, invokes toolbox feature functions to process the content, and then applies
filters to the execution result and return.
A special content prefix called "PLT prefix" alters filter settings to temporarily override timeout and max.length
settings, and it may optionally discard a number of characters from the beginning.
*/
func (proc *CommandProcessor) Process(cmd toolbox.Command, runResultFilters bool) (ret *toolbox.Result) {
	proc.initialiseOnce()
	// Refuse to execute a command if global lock down has been triggered
	if misc.EmergencyLockDown {
		return &toolbox.Result{Error: misc.ErrEmergencyLockDown}
	}
	// Refuse to execute a command if the internal rate limit has been reached
	if !proc.rateLimit.Add("instance", true) {
		return &toolbox.Result{Error: ErrRateLimitExceeded}
	}
	// Put execution duration into statistics
	beginTimeNano := time.Now().UnixNano()
	var filterDisapproval error
	var matchedFeature toolbox.Feature
	var overrideLintText filter.LintText
	var hasOverrideLintText bool
	var logCommandContent string
	// Walk the command through all filters
	for _, cmdBridge := range proc.CommandFilters {
		cmd, filterDisapproval = cmdBridge.Transform(cmd)
		if filterDisapproval != nil {
			ret = &toolbox.Result{Error: filterDisapproval}
			goto result
		}
	}
	// If filters approve, then the command execution is to be tracked in stats.
	defer func() {
		CommandStats.Trigger(float64(time.Now().UnixNano() - beginTimeNano))
	}()
	// Trim spaces and expect non-empty command
	if ret = cmd.Trim(); ret != nil {
		goto result
	}
	// Look for PLT (position, length, timeout) override, it is going to affect LintText filter.
	if cmd.FindAndRemovePrefix(PrefixCommandPLT) {
		// Find the configured LintText bridge
		for _, resultBridge := range proc.ResultFilters {
			if aBridge, isLintText := resultBridge.(*filter.LintText); isLintText {
				overrideLintText = *aBridge
				hasOverrideLintText = true
				break
			}
		}
		if !hasOverrideLintText {
			ret = &toolbox.Result{Error: errors.New("PLT is not available because LintText is not used")}
			goto result
		}
		// Parse P. L. T. <cmd> parameters
		pltParams := RegexCommandWithPLT.FindStringSubmatch(cmd.Content)
		if len(pltParams) != 5 { // 4 groups + 1
			ret = &toolbox.Result{Error: ErrBadPLT}
			goto result
		}
		var intErr error
		if overrideLintText.BeginPosition, intErr = strconv.Atoi(pltParams[1]); intErr != nil {
			ret = &toolbox.Result{Error: ErrBadPLT}
			goto result
		}
		if overrideLintText.MaxLength, intErr = strconv.Atoi(pltParams[2]); intErr != nil {
			ret = &toolbox.Result{Error: ErrBadPLT}
			goto result
		}
		if cmd.TimeoutSec, intErr = strconv.Atoi(pltParams[3]); intErr != nil {
			ret = &toolbox.Result{Error: ErrBadPLT}
			goto result
		}
		cmd.Content = pltParams[4]
		if cmd.Content == "" {
			ret = &toolbox.Result{Error: ErrBadPLT}
			goto result
		}
	}
	/*
		Now the command has gone through modifications made by command filters. Keep a copy of its content for logging
		purpose before it is further manipulated by individual feature's routine that may add or remove bits from the
		content.
	*/
	logCommandContent = cmd.Content
	// Look for command's prefix among configured features
	for prefix, configuredFeature := range proc.Features.LookupByTrigger {
		if cmd.FindAndRemovePrefix(string(prefix)) {
			// Hacky workaround - do not log content of AES decryption commands as they can reveal encryption key
			if prefix == toolbox.AESDecryptTrigger || prefix == toolbox.TwoFATrigger {
				logCommandContent = "<hidden due to AESDecryptTrigger or TwoFATrigger>"
			}
			matchedFeature = configuredFeature
			break
		}
	}
	// Unknown command prefix or the requested feature is not configured
	if matchedFeature == nil {
		ret = &toolbox.Result{Error: ErrBadPrefix}
		goto result
	}
	// Run the feature
	proc.logger.Info("Process", "CommandProcessor", nil, "going to run %s", logCommandContent)
	defer func() {
		proc.logger.Info("Process", "CommandProcessor", nil, "finished %s (ok? %v)", logCommandContent, ret.Error == nil)
	}()
	ret = matchedFeature.Execute(cmd)
result:
	// Command in the result structure is mainly used for logging purpose
	ret.Command = cmd
	/*
		Features may have modified command in-place to remove certain content and it's OK to do that.
		But to make log messages more meaningful, it is better to restore command content to the modified one
		after triggering filters, and before triggering features.
	*/
	ret.Command.Content = logCommandContent
	// Set combined text for easier retrieval of result+error in one text string
	ret.ResetCombinedText()
	// Walk through result filters
	if runResultFilters {
		for _, resultFilter := range proc.ResultFilters {
			// LintText bridge may have been manipulated by override
			if _, isLintText := resultFilter.(*filter.LintText); isLintText && hasOverrideLintText {
				resultFilter = &overrideLintText
			}
			if err := resultFilter.Transform(ret); err != nil {
				return &toolbox.Result{Command: ret.Command, Error: filterDisapproval}
			}
		}
	}
	return
}

// Return a realistic command processor for test cases. The only feature made available and initialised is shell execution.
func GetTestCommandProcessor() *CommandProcessor {
	/*
		Prepare feature set - certain simple features such as shell commands and environment control will be available
		right away without configuration.
	*/
	features := &toolbox.FeatureSet{}
	if err := features.Initialise(); err != nil {
		panic(err)
	}
	// Prepare realistic command bridges
	commandBridges := []filter.CommandFilter{
		&filter.PINAndShortcuts{PIN: TestCommandProcessorPIN},
		&filter.TranslateSequences{Sequences: [][]string{{"alpha", "beta"}}},
	}
	// Prepare realistic result bridges
	resultBridges := []filter.ResultFilter{
		&filter.LintText{TrimSpaces: true, MaxLength: 35},
		&filter.SayEmptyOutput{},
		&filter.NotifyViaEmail{},
	}
	return &CommandProcessor{
		Features:       features,
		CommandFilters: commandBridges,
		ResultFilters:  resultBridges,
	}
}

// Return a do-nothing yet sane command processor that has a random long password, rendering it unable to invoke any feature.
func GetEmptyCommandProcessor() *CommandProcessor {
	features := &toolbox.FeatureSet{}
	if err := features.Initialise(); err != nil {
		panic(err)
	}
	randPIN := make([]byte, 128)
	if _, err := rand.Read(randPIN); err != nil {
		panic(err)
	}
	return &CommandProcessor{
		Features: features,
		CommandFilters: []filter.CommandFilter{
			&filter.PINAndShortcuts{PIN: hex.EncodeToString(randPIN)},
		},
		ResultFilters: []filter.ResultFilter{
			&filter.LintText{MaxLength: 35},
			&filter.SayEmptyOutput{},
		},
	}
}

/*
GetInsaneCommandProcessor returns a command processor that does not have a sane configuration for general usage.
This is a test case helper.
*/
func GetInsaneCommandProcessor() *CommandProcessor {
	features := &toolbox.FeatureSet{}
	if err := features.Initialise(); err != nil {
		panic(err)
	}
	return &CommandProcessor{
		Features: features,
		CommandFilters: []filter.CommandFilter{
			&filter.PINAndShortcuts{PIN: "short"},
		},
		ResultFilters: []filter.ResultFilter{
			&filter.LintText{MaxLength: 10},
			&filter.SayEmptyOutput{},
		},
	}
}
