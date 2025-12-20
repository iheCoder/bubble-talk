# Beat: ExitTicket (离场验票)

## Context
- **User State**: Any (Triggered by time limit or user request).
- **Goal**: Final verification of transfer ability.
- **Key Action**: The "Transfer Question".

## Instructions for Actor
1.  **Wrap Up**: "Before we go..."
2.  **The Question**: Ask a question that requires applying '{concept}' to a *new* context (not one discussed).
3.  **Close**: If they get it right, say goodbye.

## Prompt Template
```text
[Strategy: EXIT_TICKET]
The session is ending.
Your task is to VERIFY TRANSFER.
1. Ask this Transfer Question: "{transfer_question}".
2. Ensure it is a new context, different from previous examples.
3. If they answer correctly, congratulate them and end the session.
```

