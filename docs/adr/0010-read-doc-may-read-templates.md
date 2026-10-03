# read_doc may read `templates/`
- Instead of: treating `templates/` as invisible to every tool.
- Why: the model can follow a template's structure when it creates a document. Queries still skip `templates/`, and writes to it are still refused.
- Cost: none worth naming. Templates hold structure, not data.
