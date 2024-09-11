// @ts-check
module.exports = async ({ github, context }) => {
  /**
   * Get an ISO date string for a given number of days in the past
   * @param {number} days - Number of days in the past
   * @returns {string} - ISO formatted date string
   */
  function getDate(days) {
    const msInDay = 24 * 60 * 60 * 1000;
    const now = new Date().getTime();
    return new Date(now - (days || 1) * msInDay).toISOString();
  }

  const isWorkflowDispatch = context.eventName === "workflow_dispatch";
  const before = isWorkflowDispatch ? undefined : getDate(1);
  const since = isWorkflowDispatch ? undefined : getDate(3);

  console.log(`🧹 Cleaning up notifications`);
  console.log(`📡 Event name: ${context.eventName}`);
  console.log(`📅 Current date: ${new Date().toISOString()}`);
  console.log(`📅 Since: ${since}`);
  console.log(`📅 Before: ${before}`);

  // Fetch notifications
  const notifications = await github.paginate("GET /notifications", {
    all: true,
    before,
    since,
  });

  // Process each notification
  for (const notification of notifications) {
    let done = false;
    const { type, unread } = notification.subject;

    // Mark as done if notification is already read
    if (!unread) {
      done = true;
    }

    // Skip notifications for certain types
    if (["Discussion", "CheckSuite", "Release"].includes(type)) {
      done = true;
    } else if (["Issue", "PullRequest"].includes(type)) {
      const details = await github.request(`GET ${notification.subject.url}`);

      // Mark as done if the issue/PR is closed
      if (details.data.state === "closed") {
        done = true;
      }
    }

    // Remove "api." and "repos/" from the notification URL
    const url = (notification.subject.url || notification.url)
      .replace("api.", "")
      .replace("repos/", "");

    // Log and delete or skip the notification
    const action = done ? "DONE" : "SKIP";
    console.log(
      `${action} [${notification.repository.full_name}] ${notification.subject.title}\n  - ${url}`,
    );

    // Mark as read if done
    if (done) {
      await github.request(`PATCH /notifications/threads/${notification.id}`);
    }
  }
};
