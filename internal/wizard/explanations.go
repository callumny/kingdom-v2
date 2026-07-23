package wizard

import "strings"

type configurationTopic struct {
	terms       []string
	explanation string
}

var configurationTopics = []configurationTopic{
	{
		terms: []string{"concurrent workers", "worker concurrency"},
		explanation: "Concurrent workers is the maximum number of Worker tasks Kingdom may run at the same time. " +
			"It is not a time limit or a tool-call limit; extra tasks wait for a slot. " +
			"More concurrent work can be faster, but uses more RAM and processing capacity.",
	},
	{
		terms: []string{"council members", "council size", "reviewers"},
		explanation: "Council members sets the number of independent reviewers Kingdom consults after the Workers finish. " +
			"More reviewers can surface additional gaps or disagreements, but require more model calls, memory and time.",
	},
	{
		terms: []string{"council model"},
		explanation: "Council model is the selected local model used by every Council reviewer. " +
			"It reads the original prompt and all Worker outcomes, then returns findings to the King before the final answer.",
	},
	{
		terms: []string{"separate ollama", "ollama servers", "shared ollama", "ollama server mode", "what is ollama", "what does ollama"},
		explanation: "Using separate Ollama servers gives each selected Ollama model its own local server and port. " +
			"This reduces contention during parallel work, but normally uses more RAM than one shared server.",
	},
	{
		terms: []string{"mlx servers", "mlx server", "what is mlx", "what does mlx"},
		explanation: "MLX runs one selected model per local server and port. " +
			"Kingdom starts those OpenAI-compatible servers and routes each role to the correct model.",
	},
	{
		terms: []string{"base port", "provider port", "ports"},
		explanation: "A base port is the first loopback port Kingdom uses for a local provider. " +
			"When models need separate servers, Kingdom assigns additional available ports from that starting point.",
	},
	{
		terms: []string{"king model", "king role", "what does king", "what does the king", "what is king", "what is the king"},
		explanation: "King model is the model that coordinates the run and produces the final user-facing answer. " +
			"The most capable selected model is usually the best choice.",
	},
	{
		terms: []string{"worker model", "worker role", "workers"},
		explanation: "Worker model handles focused tasks delegated by the King. " +
			"A smaller or faster model often works well because several Worker tasks may run concurrently.",
	},
	{
		terms: []string{"council", "council role"},
		explanation: "Council is an optional review stage between the Workers and the King's final synthesis. " +
			"Reviewers identify gaps, conflicting outcomes and weak evidence so the King can resolve them before answering.",
	},
}

// configurationExplanation handles factual setup questions without relying on
// the local Wizard model. This keeps explanations accurate and guarantees that
// asking for help cannot mutate the setup draft.
func configurationExplanation(message string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(message))
	if !isConfigurationQuestion(normalized) {
		return "", false
	}

	for _, topic := range configurationTopics {
		if containsAny(normalized, topic.terms...) {
			return topic.explanation + " No settings changed.", true
		}
	}
	if containsAny(normalized, "configuration", "setting", "option", "field") {
		return "King and Worker models choose who coordinates and who handles delegated tasks. " +
			"Council settings control optional review. Concurrent workers controls parallel task capacity. " +
			"Provider server and port settings control how local model processes are routed. No settings changed.", true
	}
	return "", false
}

func isConfigurationQuestion(message string) bool {
	return hasAnyPrefix(message,
		"what does ",
		"what do ",
		"what is ",
		"what are ",
		"how does ",
		"how do ",
		"why use ",
		"explain ",
		"tell me about ",
	) || containsAny(message, " mean", "meaning of", "explain the", "can you explain", "could you explain", "please explain", "why would i use")
}
