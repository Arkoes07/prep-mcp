# Tag checks stay quiet without a vocabulary (M2)
- Instead of: warning about every tag when the tags file is missing or has no tags.
- Why: with nothing to compare against there is no closest tag to name, and a warning that cannot help is noise.
- Cost: a vocabulary file that exists but is empty turns the check off.
