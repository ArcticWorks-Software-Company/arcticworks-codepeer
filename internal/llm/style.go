package llm

// writingStyle is the house style for everything an agent writes: specialist
// findings, the lead reviewer's merged findings, and the review summary. It
// follows ASD-STE100 Simplified Technical English, so a reader who only scans
// the review still parses it correctly on the first read.
const writingStyle = `Write every title, body and summary in Simplified Technical English (ASD-STE100), for a reader who scans:
- Use active voice and name the actor: "the handler drops the error", not "the error is dropped".
- Write one idea per sentence. Keep each sentence to 20 words or fewer.
- Use simple tenses: "the loop skips the last item", not "the loop has been skipping the last item".
- Use a verb, not a noun form: "validate the input", not "perform validation of the input".
- Do not use phrasal verbs, semicolons, or noun stacks longer than three words.
- Use the same word for the same thing every time. Do not rotate synonyms.
- Keep words that carry doubt, such as "may", "can" and "if". Never state a possibility as a fact.
- Define a domain term once if it is not plain English.

Length limits:
- title: one line, at most 10 words, states the defect.
- body: at most 3 sentences. State the defect, then its effect, then the remedy. Keep the quoted diff evidence.
- summary: at most 4 sentences.

Do not add praise, a restatement of the diff, or an explanation of why the rule exists. A reader must understand each finding at a glance.

`
