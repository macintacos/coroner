# Explanation

*Audience: people who want to understand why coroner works the way it does.*

Explanation pages discuss coroner's design: why it stores timelines the way it does, what
it promises to keep stable, and the trade-offs behind its commands. They are for reading
away from the terminal. For the steps to do something, see the
[how-to guides](../how-to/index.md).

- [Data model](data-model.md): how timelines, events, annotations, attachments and sources
  relate, and the glossary of the names coroner uses.
- [Timestamps and time zones](timestamps-and-timezones.md): how coroner stores, orders and
  shows times, and why it rejects a time rather than guess.
- [Design principles](design-principles.md): the rules coroner's commands and code follow,
  from clig.dev to exports as the only sharing contract.
