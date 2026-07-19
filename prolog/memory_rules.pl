% go-prolog-mcp — Memory Consistency Verification Rules
% ======================================================
%
% Default rules for the `memory_verify` MCP tool.  Facts supplied at runtime:
%     mem(Key, Category, Attribute, Value).
%
% The runtime also injects `today('YYYY-MM-DD').` so rules can compare dates.
%
% Rules are split into:
%   - contradiction/2  : strict conflicts that should never happen
%   - anomaly/2        : suspicious states that need review

%% ---------------------------------------------------------------------------
%% Contradictions
%% ---------------------------------------------------------------------------

% Feature/status says completed, but the docs category still says stale.
contradiction(Key, 'completed-vs-stale-docs') :-
    mem(Key, _, 'status', 'completed'),
    mem(Key, 'docs', 'status', 'stale').

%% ---------------------------------------------------------------------------
%% Anomalies
%% ---------------------------------------------------------------------------

% Goal is still active, has a deadline that already passed, and shows zero progress.
% Deadlines must be in ISO 8601 format ('YYYY-MM-DD') for lexicographic comparison.
anomaly(Key, 'expired-goal') :-
    mem(Key, 'goal', 'status', 'active'),
    mem(Key, 'goal', 'deadline', Deadline),
    today(Today),
    Deadline @< Today,
    mem(Key, 'goal', 'progress', '0').

% Different result values attached to the same key.
% Use @< to report each conflicting pair only once.
anomaly(Key, 'conflicting-results') :-
    mem(Key, _, 'result', ValueA),
    mem(Key, _, 'result', ValueB),
    ValueA @< ValueB.

% Baron is assigned to a goal but no progress has been made.
anomaly(Key, 'baron-idle') :-
    mem(Key, 'goal', 'assignee', 'baron'),
    mem(Key, 'goal', 'progress', '0').

% Same key has contradicting status values (e.g. myth vs idea, active vs completed).
anomaly(Key, 'dual-status') :-
    mem(Key, _, 'status', ValueA),
    mem(Key, _, 'status', ValueB),
    ValueA @< ValueB.
