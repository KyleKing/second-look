package conversations

// lightQuery asks only what says whether the conversations held for a pull
// request are still current: its identity, when it last moved, and its head.
const lightQuery = `query($q:String!,$n:Int!){
  viewer{login}
  search(query:$q,type:ISSUE,first:$n){
    nodes{
      ... on PullRequest{
        id
        number
        updatedAt
        headRefOid
        repository{nameWithOwner}
      }
    }
  }
}`

// heavyQuery asks for all three conversation surfaces of the pull requests the
// light read found changed.
//
// __typename on each author is how a machine account is told from a person.
// GitHub answers "Bot" for an App, which is exact where matching a login
// against a list of known bot names is a guess that goes stale.
//
// Every comment carries a node id and its reaction groups, because a thumbs-up
// is the standing marker for handled on all three surfaces. The node id is what
// addReaction takes, and no REST endpoint reacts to a review body at all.
//
// The ids are named nodes rather than ids so a stand-in forge can route this
// call apart from the inbox's facts read.
const heavyQuery = `query($nodes:[ID!]!){
  viewer{login}
  nodes(ids:$nodes){
    ... on PullRequest{
      number
      title
      url
      isDraft
      headRefName
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
          comments(first:50){
            nodes{
              id
              databaseId
              body
              createdAt
              author{login __typename}
              reactionGroups{content viewerHasReacted}
            }
          }
        }
      }
      comments(first:50){
        nodes{
          id
          databaseId
          body
          createdAt
          author{login __typename}
          reactionGroups{content viewerHasReacted}
        }
      }
      reviews(first:50){
        nodes{
          id
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
}`
