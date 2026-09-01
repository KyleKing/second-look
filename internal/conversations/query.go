package conversations

// query asks for the viewer, the candidate pull requests, and all three
// conversation surfaces in one request.
//
// __typename on each author is how a machine account is told from a person.
// GitHub answers "Bot" for an App, which is exact where matching a login
// against a list of known bot names is a guess that goes stale.
//
// The reaction groups are only asked for where they mean something: a review
// thread is resolved through resolveReviewThread, while a conversation comment
// and a review body can only be marked handled by reacting to them.
const query = `query($q:String!,$n:Int!){
  viewer{login}
  search(query:$q,type:ISSUE,first:$n){
    nodes{
      ... on PullRequest{
        number
        title
        url
        isDraft
        repository{nameWithOwner}
        author{login __typename}
        reviewThreads(first:50){
          nodes{
            id
            isResolved
            isOutdated
            path
            line
            diffSide
            comments(first:50){nodes{databaseId body createdAt author{login __typename}}}
          }
        }
        comments(first:50){
          nodes{
            databaseId
            body
            createdAt
            author{login __typename}
            reactionGroups{content viewerHasReacted}
          }
        }
        reviews(first:50){
          nodes{
            databaseId
            body
            createdAt
            state
            author{login __typename}
            reactionGroups{content viewerHasReacted}
          }
        }
      }
    }
  }
}`
