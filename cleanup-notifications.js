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

  function isLessThan(updatedAt, hours) {
    const now = new Date().getTime();
    const notificationTime = new Date(updatedAt).getTime();
    const diffInHours = (now - notificationTime) / (1000 * 60 * 60); // ms to hours
    return diffInHours < hours;
  }

  const isWorkflowDispatch = context.eventName === "workflow_dispatch";
  const before = undefined;
  const since = isWorkflowDispatch ? getDate(14) : getDate(3);

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
    const { type, unread, updated_at } = notification.subject;

    // Mark as done if notification is already read
    if (!unread) {
      console.log(`ALREADY READ [${notification.repository.full_name}] ${notification.subject.title}`);
      done = true;
    }

    // Skip notifications for certain types
    if (["Issue", "PullRequest"].includes(type)) {
      const details = await github.request(`GET ${notification.subject.url}`);

      // Mark as done if the issue/PR is closed
      if (details.data.state === "closed") {
        console.log(`CLOSED [${notification.repository.full_name}] ${notification.subject.title}`);
        done = true;
      }
    }

    // Remove "api." and "repos/" from the notification URL
    const url = (notification.subject.url || notification.url)
      .replace("api.", "")
      .replace("repos/", "");

    // Log and process the notification
    if (done) {
      if (isLessThan(updated_at, 3)) {
        console.log(
          `MARK AS READ [${notification.repository.full_name}] ${notification.subject.title}\n  - ${url}`,
        );
        await github.request(`PATCH /notifications/threads/${notification.id}`);
      } else {
        console.log(
          `DONE [${notification.repository.full_name}] ${notification.subject.title}\n  - ${url}`,
        );
        await github.request(`DELETE /notifications/threads/${notification.id}`);
      }
    } else {
      console.log(
        `SKIP [${notification.repository.full_name}] ${notification.subject.title}\n  - ${url}`,
      );
    }
  }
};
