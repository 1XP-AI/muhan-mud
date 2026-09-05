-- 2026-09-22: commit the additive lifecycle value before any transaction can
-- install a constraint or function which uses it.
alter type public.character_lifecycle add value if not exists 'handoff_pending';
