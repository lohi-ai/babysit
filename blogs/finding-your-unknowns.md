# Finding Your Unknowns: You Can't Prompt for What You Don't Know Exists

*A summary of Thariq's "A Field Guide to Fable" — why unknown unknowns, not the model, are now the bottleneck in AI-assisted coding.*

Working with Claude Fable 5 keeps re-teaching an old lesson: **the map is not the territory.**

The map is what you give Claude — your prompts, skills, and context, a representation of the work to be done. The territory is where the work actually happens: the codebase, the real world, its actual constraints. The difference between the two is your **unknowns**. When Claude hits an unknown, it makes a decision based on its best guess of what you want — and the more work being done, the more unknowns it will hit.

Fable is the first model where the quality of the work is bottlenecked not by the model, but by **your ability to clarify its unknowns**. You cannot prompt for something you don't know exists.

Importantly, planning ahead isn't always enough. Unknowns surface deep in implementation, and sometimes an unknown reveals that you should be solving a different problem altogether. Working with Fable is an iterative process of discovering your unknowns **before, during, and after** implementation.

## Knowing your unknowns

Break every problem down four ways:

| Quadrant | Definition |
|---|---|
| Known knowns | What's in your prompt — what you tell the agent you want |
| Known unknowns | What you haven't figured out yet, and know you haven't |
| Unknown knowns | So obvious you'd never write it down — but you'd recognize it on sight |
| Unknown unknowns | What you haven't considered at all. Do you even know how good something can be? |

The best agentic coders have relatively few unknowns — they're deeply in sync with both the codebase and the model's behaviors. But they also *assume* unknowns exist. Reducing and planning for your unknowns **is** the skill of agentic coding, and it's a skill you can improve by working with Claude itself.

## Help Claude help you

Instructing Claude is a delicate balance. Too specific, and Claude follows your instructions even when a pivot would be better. Too vague, and Claude fills the gaps with industry best practices that may not fit your task. When you don't account for your unknowns, you fail both ways: you don't know when the path will be full of obstacles, and you don't know when the path is clear but you still want Claude to veer.

Claude can help you discover your unknowns faster — it searches your codebase and the internet quickly, knows more than you about the average topic, and iterates from failure faster. The most important input is **context about your starting point**: where you are in your thought process, your experience with the problem and the codebase. Let it work with you like a thought partner.

## Before implementation

**Blindspot pass.** Starting work in an unfamiliar part of the codebase, you're loaded with unknown unknowns — you don't know what questions to ask, what good looks like, what historical work exists, or what potholes to avoid. Ask Claude directly, using the literal words: *"Do a blindspot pass to help me find my unknown unknowns and help me prompt you better."* Tell it who you are and what you already know.

**Brainstorms and prototypes.** For areas full of unknown knowns — criteria you can only define when you see them — brainstorm and prototype first. Finding them during implementation is expensive: small spec changes can cause drastically different code, and reverting is hard. Visual design is the classic case: ask for several wildly different design directions in a single HTML file and *react* to them, before anything touches the real app. Starting every session with a brainstorm also calibrates scope — Claude often finds high-value approaches you'd have missed.

**Interviews.** After brainstorming, unknowns remain. Ask Claude to interview you: *"one question at a time, prioritize questions where my answer would change the architecture."*

**References.** Sometimes you can't describe what you want — you lack the language, or it would take too long. The best reference is source code. Point Fable at a folder that implements the behavior you want — even in a different language — and tell it what to look for: *"This Rust crate in vendor/rate-limiter implements the exact backoff behavior I want. Reimplement the same semantics in our TypeScript client."*

**Implementation plans.** When you're ready to build, ask for a plan that **leads with the decisions most likely to change** — data models, type interfaces, UX flows — and buries the mechanical refactoring at the bottom. That surfaces the things you might actually need to alter.

## During implementation

**Implementation notes.** No matter how much you plan, unknown unknowns lurk. The agent will hit edge cases that force it off-plan. Have it keep a running `implementation-notes.md`: *"If an edge case forces you to deviate from the plan, pick the conservative option, log it under 'Deviations', and keep going."* Those notes are what you learn from on the next attempt.

## After implementation

**Pitches and explainers.** Shipping means buy-in. Package the prototype, the spec, and the implementation notes into one doc: reviewers start with the same unknowns you did, and experts approve faster when they can see you accounted for the failure points they'd have anticipated.

**Quizzes.** After a long session, Claude may have done far more than you realized. Reading diffs gives only surface understanding — much of the behavior depends on existing code paths the diff never shows. Ask for a report with context and intuition, plus a quiz at the bottom. **Only merge after you pass the quiz perfectly.**

## Case study: the Fable launch video

The Fable launch video was edited entirely by Claude Code — a brand-new domain for Thariq. The process was the field guide in miniature: start from known knowns (Claude can edit and transcribe video with code), ask for an explainer on the uncertain part (is Whisper accurate enough to cut "ums" with ffmpeg?), prototype the risky idea (a Remotion UI timed to the transcript), and when the output looked muted, hit the wall of unknown unknowns: color grading. A first attempt — generate variations and pick one — failed, because he didn't know what *good* grading looked like. The fix was to ask Claude to **teach** him color grading first, converting the unknown unknowns before choosing.

## Matching the map and the territory

The better models get, the more the right approach can achieve. When a long-horizon task comes back wrong, the likely cause isn't the model — it's that you needed to spend more time defining your unknowns, or writing a plan that lets Claude improvise through them.

Every explainer, brainstorm, interview, prototype, and reference is **a cheap way to find out what you didn't know, before it gets expensive to fix.** Start your next project by asking Claude to help you find your unknowns.

---

*Summary of "A Field Guide to Fable: Finding Your Unknowns" by Thariq (@trq212), x.com/trq212/status/2073100352921215386.*
