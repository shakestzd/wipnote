# ASD-STE100 Issue 9: rules in plain summary

These are original summaries, not the wording of the specification. Cite rules by number and read the
rule itself in your own copy of the spec. Numeric limits live in `scripts/ste_lint.py`
(run `uv run scripts/ste_lint.py --show-limits`); this file points at them and never repeats them.

How to read the check column:

- **S** - `ste_lint.py` checks it mechanically (a subset, or a hint only where marked).
- **D** - needs the dictionary: run `scripts/lookup.py` on the words.
- **J** - needs judgement by the writer and then a human reviewer.

## Contents

- [Section 1 - Words](#section-1---words)
- [Section 2 - Multi-word nouns](#section-2---multi-word-nouns)
- [Section 3 - Verbs](#section-3---verbs)
- [Section 4 - Sentences](#section-4---sentences)
- [Section 5 - Procedural writing](#section-5---procedural-writing)
- [Section 6 - Descriptive writing](#section-6---descriptive-writing)
- [Section 7 - Safety instructions](#section-7---safety-instructions)
- [Section 8 - Punctuation and word count](#section-8---punctuation-and-word-count)
- [Section 9 - Writing practices](#section-9---writing-practices)
- [General recommendations](#general-recommendations)
- [What the script cannot see](#what-the-script-cannot-see)

## Section 1 - Words

| Rule | Summary | Check |
|---|---|---|
| 1.1 | Every word must be in the dictionary as approved, or be a technical noun, or be a technical verb. | D, J |
| 1.2 | Use an approved word only as the part of speech the dictionary gives for it. | D |
| 1.3 | Use an approved word only in the meaning the dictionary gives for it. | D, J |
| 1.4 | Use only the verb and adjective forms the dictionary lists. | D |
| 1.5 | A word outside the dictionary is allowed as a technical noun when it fits one of the technical-noun categories in the spec. Company glossaries hold these terms. | J |
| 1.6 | A word that the dictionary marks as unapproved is allowed only when it works as a technical noun, or inside one. | D, J |
| 1.7 | Do not turn a technical noun into a verb. | J |
| 1.8 | Pick technical nouns that your company, industry or field already approves. | J |
| 1.9 | When you have a choice of technical noun, pick a short one that is easy to understand. | J |
| 1.10 | Do not use regional words, slang or jargon as technical nouns. | J |
| 1.11 | Name one item with one technical noun. Do not rotate synonyms. | J |
| 1.12 | A verb outside the dictionary is allowed as a technical verb when it fits one of the technical-verb categories in the spec. | J |
| 1.13 | Do not turn a technical verb into a noun. | J |
| 1.14 | Use American spelling unless an official directive says otherwise. | S (hint, small word set) |

## Section 2 - Multi-word nouns

| Rule | Summary | Check |
|---|---|---|
| 2.1 | Keep a multi-word noun short: the spec sets a limit on how many words it may hold. The script does not count noun clusters, because that needs part-of-speech tagging. | J |
| 2.2 | When a technical noun is longer than the limit, write it out in full once. Then make it clear with a shorter form or with hyphens that join the words used as one unit. Do not hyphenate more words than the limit allows (`MAX_HYPHEN_GROUP_WORDS`). | S (hyphen chains), J |

## Section 3 - Verbs

| Rule | Summary | Check |
|---|---|---|
| 3.1 | Use only the verb forms the dictionary gives. | D |
| 3.2 | Allowed forms: infinitive, imperative, simple present, simple past, simple future, and the past participle used as an adjective. | J, S (perfect-tense hint) |
| 3.3 | Use the past participle as an adjective, before a noun or after to be, to become or to stay. This shows a condition and is not the passive. | J |
| 3.4 | Do not build compound verb phrases with auxiliaries. Perfect tenses are an example. | S (hint), J |
| 3.5 | Use an "-ing" form only as a technical noun or as a modifier inside one. | S (hint), J |
| 3.6 | Use the active voice. Descriptive text may use the passive only when nobody knows the agent. | S (passive with an agent), J |
| 3.7 | Say an action with an approved verb, not with a noun or another word class. | J |

## Section 4 - Sentences

| Rule | Summary | Check |
|---|---|---|
| 4.1 | Write short, clear sentences. Limits are in Rules 5.1 and 6.3. | S (length), J |
| 4.2 | Do not drop words or use contractions to save space. | S (contractions), J |
| 4.3 | Put complex text in a vertical list. | J |
| 4.4 | Link related sentences with connecting words or phrases. | J |
| 4.5 | Put an article or a demonstrative adjective before a noun when the sentence needs one. General statements about a class of things need none. | J |

## Section 5 - Procedural writing

| Rule | Summary | Check |
|---|---|---|
| 5.1 | Keep each procedure sentence within the procedure limit (`MAX_WORDS_PROCEDURE`). Section 8 says how to count. | S |
| 5.2 | Give one instruction per sentence, unless two actions happen at the same time. | J |
| 5.3 | Write instructions as commands. | J |
| 5.4 | When the reader must know a condition first, state it first and divide it from the command with a comma. | J |
| 5.5 | A note gives information only: no instructions, requirements or limits, and no command form. Note sentences follow `MAX_WORDS_NOTE`. | S (length), J |

## Section 6 - Descriptive writing

| Rule | Summary | Check |
|---|---|---|
| 6.1 | Release information a little at a time. One subject per sentence. | J |
| 6.2 | Use key words and key phrases to give the text a visible structure. | J |
| 6.3 | Keep each descriptive sentence within `MAX_WORDS_DESCRIPTIVE`. | S |
| 6.4 | Use paragraphs to group related information. Open each with a topic sentence. | J |
| 6.5 | Give each paragraph one topic. | J |
| 6.6 | Keep each paragraph within `MAX_SENTENCES_PER_PARAGRAPH`. | S |

## Section 7 - Safety instructions

The spec gives no separate word limit for safety text. The script applies the procedure limit as an assumption (`MAX_WORDS_SAFETY`).

| Rule | Summary | Check |
|---|---|---|
| 7.1 | Mark the level of risk with a signal word such as warning or caution, or with a symbol. | J |
| 7.2 | Start with a clear, exact command or condition. | J |
| 7.3 | State the risk or the likely result when you can. | J |

## Section 8 - Punctuation and word count

| Rule | Summary | Check |
|---|---|---|
| 8.1 | Standard punctuation is fine, except the semicolon. | S |
| 8.2 | Join directly related words with hyphens. A hyphen is not a dash. The spec does not forbid dashes, so the script does not flag them. | J |
| 8.3 | Parentheses are allowed for a fixed list of purposes: references, identifiers, step labels, abbreviations, singular-or-plural forms, short explanations, alternatives. | J |
| 8.4 | In a vertical list, the colon ends the introducing sentence for counting. Each list item counts as its own sentence. | S |
| 8.5 | Text in parentheses counts as one word in the host sentence, and forms a separate sentence of its own. | S |
| 8.6 | These count as one word each: numbers, numbers with units, abbreviations, alphanumeric identifiers, quoted text, titles and labels, proper nouns of people, groups, organizations and geopolitical entities. | S (partial: titles, labels and proper nouns cannot be detected) |
| 8.7 | A hyphenated word counts as one word. | S |

## Section 9 - Writing practices

| Rule | Summary | Check |
|---|---|---|
| 9.1 | When a one-for-one word swap does not work, rebuild the sentence. | J |
| 9.2 | Check the approved meaning of each word before use. Standard-English senses may be unapproved. | D, J |
| 9.3 | Do not form phrasal verbs from a verb plus a particle. | D (many are listed as unapproved), J |
| 9.4 | Use the same wording each time the same kind of step recurs. | J |

## General recommendations

| Item | Summary | Check |
|---|---|---|
| GR-1 | Keep the conjunction "that" after verbs such as "make sure", so the clause boundary is clear. | J |
| GR-2 | The preposition "with" can be read several ways. Reword when it is ambiguous. | J |
| GR-3 | Use only the pronouns the dictionary approves. Make the referent clear. | S (gendered pronouns), D, J |
| GR-4 | Make sure the reader knows what "this" points to. | J |
| GR-5 | Beware of words that look like words in the writer's own language but differ in English. | J |
| GR-6 | Do not use Latin abbreviations. Write the English words. | S |
| GR-7 | Use gender-neutral language. Gender-specific pronouns are not permitted. | S (pronouns), J |
| GR-8 | The possessive "'s" is allowed. Avoid it when unsure it is correct. | J |

## What the script cannot see

The linter reads plain text and uses patterns. It cannot see part of speech, meaning, sense of a word, noun-cluster length, missing articles, imperative mood, one-instruction-per-sentence, or whether a note smuggles in an instruction. A clean run proves nothing about those. Passing it never means the text complies.
