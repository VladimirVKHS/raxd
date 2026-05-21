# Review: <короткое название задачи>

## Summary
Один абзац (3-6 строк): общая оценка — насколько код соответствует `spec.md`/`plan.md`/
`security-requirements.md`, сколько issues и какой уровень (обязательные / для обсуждения), итоговый
verdict одной фразой.

## Issues
Каждый issue — только обоснованный (нарушен AC / контракт plan / пункт baseline), без вкусовщины.

- [ ] **Issue: <короткая формулировка>**
  - Где: `<path>:<line>`
  - Почему: какой acceptance criterion / контракт из `plan.md` / пункт `SECURITY-BASELINE.ru.md`
    нарушен и чем это грозит.
  - Что делать: конкретное действие для developer (1-3 строки).

## Looks good
- Что сделано хорошо (1-4 пункта), чтобы developer не «чинил» исправное и переиспользовал удачные решения.

## Verdict
accept | needs-changes | needs-discussion

- `accept` — все acceptance criteria закрыты, обязательных issues нет.
- `needs-changes` — есть issues, требующие правок до merge (перечислены выше).
- `needs-discussion` — нужно решение пользователя/команды (неоднозначность, trade-off вне контракта).
