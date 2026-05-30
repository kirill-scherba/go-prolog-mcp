% Go Prolog MCP — Workflow Verification Rules
% ============================================
%
% Facts:
%   scenario(Name, FromStatus, ToStatus, RequiredLabels, WithoutLabels).
%   board_status(Name).

%% ---------------------------------------------------------------------------
%% 1. CONFLICT: two scenarios from the same status can match the same item.
%% ---------------------------------------------------------------------------
conflict(From, A, B) :-
    scenario(A, From, _, ReqA, WithoutA),
    scenario(B, From, _, ReqB, WithoutB),
    A @< B,
    common_required(ReqA, ReqB),
    \+ blocked_by_without(ReqA, WithoutB),
    \+ blocked_by_without(ReqB, WithoutA).

common_required(L1, L2) :-
    member(Lbl, L1),
    member(Lbl, L2).

blocked_by_without(Req, Without) :-
    member(Lbl, Req),
    member(Lbl, Without).

%% ---------------------------------------------------------------------------
%% 2. DEADLOCK: a status with no outgoing scenario (except Done).
%% ---------------------------------------------------------------------------
deadlock(Status) :-
    board_status(Status),
    Status \= 'Done',
    \+ scenario(_, Status, _, _, _).

%% ---------------------------------------------------------------------------
%% 3. UNREACHABLE SCENARIO: trigger_status never entered by other scenarios.
%% ---------------------------------------------------------------------------
unreachable(Name) :-
    scenario(Name, From, _, _, _),
    From \= 'Backlog',
    \+ scenario(_, _, From, _, _).

%% ---------------------------------------------------------------------------
%% 4. PATH (cycle-safe): transitive closure with visited set.
%% ---------------------------------------------------------------------------
path(From, To) :-
    path_(From, To, []).

path_(From, To, _) :-
    scenario(_, From, To, _, _).
path_(From, To, Visited) :-
    scenario(_, From, Mid, _, _),
    \+ member(Mid, Visited),
    path_(Mid, To, [Mid | Visited]).

%% ---------------------------------------------------------------------------
%% 5. CYCLE: a status that can reach itself.
%% ---------------------------------------------------------------------------
cycle(Status) :-
    scenario(_, Status, _, _, _),
    scenario(_, Status, Next, _, _),
    path_(Next, Status, [Status]).
