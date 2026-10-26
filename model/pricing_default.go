package model

import (
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/pkg/catalogsync"
)

// 简化的供应商映射规则
var defaultVendorRules = map[string]string{
	"gpt":      "OpenAI",
	"dall-e":   "OpenAI",
	"whisper":  "OpenAI",
	"o1":       "OpenAI",
	"o3":       "OpenAI",
	"claude":   "Anthropic",
	"gemini":   "Google",
	"moonshot": "MoonshotAI",
	"kimi":     "MoonshotAI",
	"chatglm":  "Z.ai",
	"glm-":     "Z.ai",
	"qwen":     "Qwen",
	"deepseek": "DeepSeek",
	"abab":     "MiniMax",
	"minimax":  "MiniMax",
	"ernie":    "Baidu",
	"spark":    "iFlytek",
	"hunyuan":  "Tencent",
	"command":  "Cohere",
	"@cf/":     "Cloudflare",
	"360":      "360",
	"yi":       "01.AI",
	"jina":     "Jina",
	"mistral":  "Mistral",
	"grok":     "xAI",
	"llama":    "Meta",
	"doubao":   "ByteDance",
	"kling":    "Kling",
	"jimeng":   "Jimeng",
	"vidu":     "Vidu",
}

// 供应商默认图标映射
var defaultVendorIcons = map[string]string{
	"OpenAI":       "OpenAI",
	"Anthropic":    "Claude.Color",
	"Google":       "Gemini.Color",
	"MoonshotAI":   "Moonshot",
	"Z.ai":         "Zhipu.Color",
	"Alibaba":      "Qwen.Color",
	"Qwen":         "Qwen.Color",
	"DeepSeek":     "DeepSeek.Color",
	"MiniMax":      "Minimax.Color",
	"Baidu":        "Wenxin.Color",
	"iFlytek":      "Spark.Color",
	"Tencent":      "Hunyuan.Color",
	"Cohere":       "Cohere.Color",
	"Cloudflare":   "Cloudflare.Color",
	"360":          "Ai360.Color",
	"01.AI":        "Yi.Color",
	"Jina":         "Jina",
	"Mistral":      "Mistral.Color",
	"xAI":          "XAI",
	"Meta":         "Ollama",
	"ByteDance":    "Doubao.Color",
	"Kling":        "Kling.Color",
	"Jimeng":       "Jimeng.Color",
	"Vidu":         "Vidu",
	"Microsoft":    "AzureAI",
	"Microsoft AI": "AzureAI",
	"Azure":        "AzureAI",
}

// initDefaultVendorMapping 简化的默认供应商映射
func initDefaultVendorMapping(metaMap map[string]*Model, vendorMap map[int]*Vendor, enableAbilities []AbilityWithChannel) {
	patterns := make([]string, 0, len(defaultVendorRules))
	for pattern := range defaultVendorRules {
		patterns = append(patterns, pattern)
	}
	slices.SortFunc(patterns, func(a, b string) int {
		if len(a) != len(b) {
			return len(b) - len(a)
		}
		return strings.Compare(a, b)
	})
	for _, ability := range enableAbilities {
		modelName := ability.Model
		if _, exists := metaMap[modelName]; exists {
			continue
		}

		// 匹配供应商
		vendorID := 0
		modelLower := strings.ToLower(modelName)
		for _, pattern := range patterns {
			vendorName := defaultVendorRules[pattern]
			if strings.Contains(modelLower, pattern) {
				vendorID = getDisplayVendor(vendorName, vendorMap)
				break
			}
		}

		// 创建模型元数据
		metaMap[modelName] = &Model{
			ModelName: modelName,
			VendorID:  vendorID,
			Status:    1,
			NameRule:  NameRuleExact,
		}
	}
}

// Default vendor entries are presentation data. Reading pricing must never
// recreate a deleted/merged database record.
var defaultVendorDisplayIDs = map[string]int{
	"360":                   -1001,
	"Anthropic":             -1002,
	"Cloudflare":            -1003,
	"Cohere":                -1004,
	"DeepSeek":              -1005,
	"Google":                -1006,
	"Jina":                  -1007,
	"Meta":                  -1008,
	"MiniMax":               -1009,
	"Mistral":               -1010,
	"MoonshotAI":            -1011,
	"OpenAI":                -1012,
	"Vidu":                  -1013,
	"xAI":                   -1014,
	"Jimeng":                -1015,
	"ByteDance":             -1016,
	"Kling":                 -1017,
	"Z.ai":                  -1018,
	"Baidu":                 -1019,
	"Tencent":               -1020,
	"iFlytek":               -1021,
	"Alibaba":               -1022,
	"01.AI":                 -1023,
	"AionLabs":              -1024,
	"Amazon":                -1025,
	"Arcee AI":              -1026,
	"AssemblyAI":            -1027,
	"BAAI":                  -1028,
	"Black Forest Labs":     -1029,
	"ByteDance Seed":        -1030,
	"Canopy Labs":           -1031,
	"Venice":                -1032,
	"Deepgram":              -1033,
	"Dots Studio":           -1034,
	"ElevenLabs":            -1035,
	"Elorian":               -1036,
	"Fireworks":             -1037,
	"Fish Audio":            -1038,
	"hexgrad":               -1039,
	"HeyGen":                -1040,
	"IBM":                   -1041,
	"Inception":             -1042,
	"inclusionAI":           -1043,
	"Inference.net":         -1044,
	"Intfloat":              -1045,
	"Jared Palmer":          -1046,
	"Krea":                  -1047,
	"LiquidAI":              -1049,
	"Mancer":                -1050,
	"Meituan":               -1051,
	"Microsoft AI":          -1052,
	"Moondream":             -1053,
	"Morph":                 -1055,
	"Nace.AI":               -1056,
	"Nex AGI":               -1057,
	"Nous":                  -1058,
	"NVIDIA":                -1059,
	"OpenRouter":            -1060,
	"Perceptron":            -1061,
	"Perplexity":            -1062,
	"Poolside":              -1063,
	"PrismML":               -1064,
	"Qwen":                  -1065,
	"Recraft":               -1066,
	"Reflection":            -1067,
	"Relace":                -1068,
	"Respan":                -1069,
	"Runway":                -1070,
	"Sakana":                -1071,
	"Sao10K":                -1072,
	"ScaleDown":             -1073,
	"Sentence Transformers": -1074,
	"Sesame":                -1075,
	"Sourceful":             -1076,
	"StepFun":               -1077,
	"TheDrummer":            -1078,
	"Thenlper":              -1079,
	"Thinking Machines":     -1080,
	"Together":              -1081,
	"TypeSafe":              -1082,
	"Upstage":               -1083,
	"VoyageAI by MongoDB":   -1084,
	"Writer":                -1085,
	"Xiaomi":                -1086,
	"XingChen":              -1087,
}

// catalogAuthorVendorNames maps getapi-catalog author namespaces to the
// author names OpenRouter shows as the model-name prefix; "~" aliases share
// their base namespace.
var catalogAuthorVendorNames = map[string]string{
	"aion-labs":             "AionLabs",
	"alibaba":               "Alibaba",
	"amazon":                "Amazon",
	"anthropic":             "Anthropic",
	"arcee-ai":              "Arcee AI",
	"assemblyai":            "AssemblyAI",
	"baai":                  "BAAI",
	"black-forest-labs":     "Black Forest Labs",
	"bytedance":             "ByteDance",
	"bytedance-seed":        "ByteDance Seed",
	"canopylabs":            "Canopy Labs",
	"cloudflare":            "Cloudflare",
	"cognitivecomputations": "Venice",
	"cohere":                "Cohere",
	"deepgram":              "Deepgram",
	"deepseek":              "DeepSeek",
	"dots-studio":           "Dots Studio",
	"elevenlabs":            "ElevenLabs",
	"elorian":               "Elorian",
	"fireworks":             "Fireworks",
	"fish-audio":            "Fish Audio",
	"google":                "Google",
	"hexgrad":               "hexgrad",
	"heygen":                "HeyGen",
	"ibm-granite":           "IBM",
	"inception":             "Inception",
	"inclusionai":           "inclusionAI",
	"inference-net":         "Inference.net",
	"intfloat":              "Intfloat",
	"jaredpalmer":           "Jared Palmer",
	"krea":                  "Krea",
	"kwaivgi":               "Kling",
	"liquid":                "LiquidAI",
	"mancer":                "Mancer",
	"meituan":               "Meituan",
	"meta":                  "Meta",
	"meta-llama":            "Meta",
	"microsoft":             "Microsoft AI",
	"minimax":               "MiniMax",
	"mistralai":             "Mistral",
	"moondream":             "Moondream",
	"moonshotai":            "MoonshotAI",
	"morph":                 "Morph",
	"nace-ai":               "Nace.AI",
	"nex-agi":               "Nex AGI",
	"nousresearch":          "Nous",
	"nvidia":                "NVIDIA",
	"openai":                "OpenAI",
	"openrouter":            "OpenRouter",
	"perceptron":            "Perceptron",
	"perplexity":            "Perplexity",
	"poolside":              "Poolside",
	"prism-ml":              "PrismML",
	"qwen":                  "Qwen",
	"recraft":               "Recraft",
	"reflection":            "Reflection",
	"relace":                "Relace",
	"respan":                "Respan",
	"runway":                "Runway",
	"sakana":                "Sakana",
	"sao10k":                "Sao10K",
	"scaledown":             "ScaleDown",
	"sentence-transformers": "Sentence Transformers",
	"sesame":                "Sesame",
	"sourceful":             "Sourceful",
	"stepfun":               "StepFun",
	"tencent":               "Tencent",
	"thedrummer":            "TheDrummer",
	"thenlper":              "Thenlper",
	"thinkingmachines":      "Thinking Machines",
	"togethercomputer":      "Together",
	"typesafe":              "TypeSafe",
	"upstage":               "Upstage",
	"voyageai":              "VoyageAI by MongoDB",
	"writer":                "Writer",
	"x-ai":                  "xAI",
	"xiaomi":                "Xiaomi",
	"xingchen-agi":          "XingChen",
	"z-ai":                  "Z.ai",
}

func getDisplayVendor(vendorName string, vendorMap map[int]*Vendor) int {
	for id, vendor := range vendorMap {
		if strings.EqualFold(vendor.Name, vendorName) {
			return id
		}
	}
	id := defaultVendorDisplayIDs[vendorName]
	if id == 0 {
		return 0
	}
	vendorMap[id] = &Vendor{Id: id, Name: vendorName, Status: 1, Icon: getDefaultVendorIcon(vendorName)}
	return id
}

// catalogAuthorVendor resolves a catalog document author to a vendor without
// creating database rows; unknown namespaces stay unassigned.
func catalogAuthorVendor(author *catalogsync.Author, vendorMap map[int]*Vendor) int {
	if author == nil {
		return 0
	}
	vendorName := catalogAuthorVendorNames[strings.TrimPrefix(strings.ToLower(strings.TrimSpace(author.Namespace)), "~")]
	if vendorName == "" {
		return 0
	}
	return getDisplayVendor(vendorName, vendorMap)
}

var contextTagPattern = regexp.MustCompile(`(?i)^\d+(\.\d+)?[KM]$`)

// withCatalogContextTag appends the catalog context window as a display tag
// ("32.8K", "1.05M") unless the stored tags already carry one.
func withCatalogContextTag(tags string, limits json.RawMessage) string {
	for tag := range strings.SplitSeq(tags, ",") {
		if contextTagPattern.MatchString(strings.TrimSpace(tag)) {
			return tags
		}
	}
	var parsed struct {
		ContextTokens int64 `json:"contextTokens"`
	}
	if json.Unmarshal(limits, &parsed) != nil || parsed.ContextTokens <= 0 {
		return tags
	}
	tag, suffix := strconv.FormatFloat(float64(parsed.ContextTokens)/1e3, 'f', 1, 64), "K"
	if parsed.ContextTokens >= 999_950 {
		tag, suffix = strconv.FormatFloat(float64(parsed.ContextTokens)/1e6, 'f', 2, 64), "M"
	}
	tag = strings.TrimSuffix(strings.TrimRight(tag, "0"), ".")
	if tag == "" || tag == "0" {
		return tags
	}
	tag += suffix
	if strings.TrimSpace(tags) == "" {
		return tag
	}
	return tags + "," + tag
}

// 获取供应商默认图标
func getDefaultVendorIcon(vendorName string) string {
	if icon, exists := defaultVendorIcons[vendorName]; exists {
		return icon
	}
	return ""
}
