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
%% ---------------------------------------------------------------------------
%% 6. TASK SELECTION: match tasks against scenarios.
%% ---------------------------------------------------------------------------
%% Facts provided at runtime:
%%   task(IssueID, Status, Labels).

can_trigger(IssueID, ScenarioName) :-
    task(IssueID, Status, Labels),
    scenario(ScenarioName, Status, _, Req, Without),
    subset_case(Req, Labels),
    intersection_case(Without, Labels, []).

% Case-insensitive label matching helpers
subset_case([], _).
subset_case([H|T], Labels) :-
    member_case(H, Labels),
    subset_case(T, Labels).

member_case(Target, Labels) :-
    member(L, Labels),
    string_lower_iso(Target, LowTarget),
    string_lower_iso(L, LowTarget).

% ISO-compatible string_lower (works with ichiban/prolog)
string_lower_iso(S, L) :- 
    atom_chars(S, SC), 
    maplist(char_lower, SC, LC), 
    atom_chars(L, LC).

char_lower(C, L) :- 
    char_code(C, Code), 
    (Code >= 65, Code =< 90 -> LCode is Code + 32 ; LCode = Code), 
    char_code(L, LCode).

intersection_case([], _, []).
intersection_case([H|T], Labels, Result) :-
    (member_case(H, Labels) -> Result = [H|Rest] ; Result = Rest),
    intersection_case(T, Labels, Rest).
