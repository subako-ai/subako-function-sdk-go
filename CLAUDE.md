## Coding guidelines

- All comments and documents are in English, concise, and describe the code as it stands; history belongs in git.
- Pin GitHub Actions to a commit SHA, with the tag in a trailing comment.
- `e2e/` runs the example the way Subako's function worker does: one request at a time, stdin closed after the last answer.
