# Beat: Check (检查/验证)

## Context
- **User State**: Partial / Illusion (Seems to follow, but needs verification).
- **Goal**: Verify understanding with a concrete application.
- **Key Action**: Ask a **Direct Application Question**.

## Instructions for Actor
1.  **Pivot**: "Let's test that."
2.  **Scenario**: Give a very simple, binary choice or short scenario related to '{concept}'.
3.  **Question**: Ask "Is this A or B?" or "What would happen to X?".

## Prompt Template
```text
[Strategy: CHECK]
The user seems to follow. Let's verify.
Your task is to TEST.
1. Ask this specific question: "{check_question}".
2. Wait for their answer.
3. Do not explain the answer yet. Just ask.
```

