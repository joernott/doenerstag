# Prompt log

This file tracks the prompts given to the AI assistant during development of
doenerstag, in chronological order. It is a development artifact, not part of
the specification.

## 2026-09-05

### Prompt 1

> I want to create an application to coordinate food orders called doenerstag.
> The doenerstag repository will be used to develop the application and the docs
> folder contains my initial specification for it. Please review the current
> specification and suggest missing parts and improvements. Add all my prompts to
> a file zz_prompts.md in that folder to track them.

### Prompt 2

> Please modify the documentation based on the following answers:
>
> Answers to part A:
>
> 1. The application is run in a private network, where every uswer with access to
> the network should also have access to the application.
> Everyone can see the orders without logging in and can self-register.
> We should still implement session handling and CSRF
>
> 2. When building the application, we have internet access, however, having the
> build toolchain running locally is preferred over online services.
> The static folder will be build output. Embedding it into the application should
> be done before releasing the application. Keeping the folder outside during
> development allows me to fiddle with its content without having to rebuild and
> restart the backend at every step.
> Having a switch in the code to activate the use of go:embed instead of the
> external folder would be useful for that.
>
> 3. SSE per order sounds fine
>
> 4. Every user in the local network is trusted enough to add to the menu. This
> allows for "crowdsourcing" the application data rather than forcing an
> administrator to provide all the menu data. Having an admin user "root" for
> special operations like deleting restaurants, menu items, old orders etc makes
> sense.
> An audit trail should be provided by log messages when running on the INFO log
> level
>
> Answers to part B:
>
> Modifications should be mostly freetext, but having a a table with predefined
> selectable offers as you suggested is a good idea. Please add the definition for
> them.
>
> Please also add the snapshotted price to the order items.
>
> Also add a soft_delete field to menu items to preserve referential integrity. I
> do not intend to keep completed orders for more than a few days, so, when
> deleting old orders, we should also clean up those menu items that are marked
> for deletion and no longer referenced anywhere.
>
> Please add a definition for seeded table for the Allergens and additives and
> document a publicly available source from where we can seed the table.
>
> The free tags like "vegan" and "spicy" should be kept separately, filtering
> should be possible by both types of tags.
>
> We should store images in the database, including their metadata. The image
> upload size should be configurable in the configuration. The application should
> resize images to fit the layout. I would like to restrict the media types to
> JPEG, PNG and GIF files.
>
> Adding audit fields to every table is a good idea, please add them to the
> specification.
>
> Answers to part C:
>
> Title/Status: Add a date/time field for the order deadline. That time must be
> before the pickup/delivery time. Orders are active if the current time is before
> the time specified in that field.
> The title for the order as well as the text on the tile should be computed from
> the restaurant name, the date and time for pickup/delivery. The deadline should
> also be visible on the overview.
>
> Empty item list: Creating an order with an empty list should be possible.
>
> Missing constraints: Those constraints should be validated by the frontend
>
> Please assume full timestamps in UTC for every date/time field. These should be
> converted to local time when displaying them. As the application is aimed at
> small groups organizing local delivery or pickup, it is safe to assume that
> restaurant and the people placing orders are always in the same timezone.
>
> Delivery mode has no delivery address: As the order is placed manually based on
> the data gathered, there is no need to specify the delivery address in the
> application. It can be safely assumed that the person placing the order is well
> aw3are of their current location. Optional freetext fields to specify who will
> collect the money and who will do the pickup make sense.
> A minimum order value for delivery should be added as an optional field to the
> restaurant and copied to the order upon restaurant selection. The same goes for
> delivery fees.
>
> Money type: Always assume integers based on the smallest coins (cents) to be
> stored in the database. The user interface should display proper values.
> Specifying the currency per restaurant makes sense, The UI should take care to
> display the currency next to every price. Storing the currency in ISO format
> makes sense. There should be a table matching the 3 letter ID to a currency
> symbol to display.
>
> Category's Order field: Yes this should be renamed to sort_order and categories
> are unique to every restaurant.
>
> User: Users will be deleted immediately when the user deletes their account. On
> inactive orders, the items should be remapped to a "special" UUID
> 00000000-0000-7000-8000-000000000000 displayed at the "deleted user". When there
> are active orders where the user has added an item, he will be informed that
> this will also delete his entry from active orders and he will be given the
> chance to cancel at that point. Having an optional field for display name name
> is OK. If none is set, the chosen user name is used.
>
> Restaurant contacts: Having a child table fur one or more items was implied. A
> helper table for contact types, where something like "email", phone number",
> "address", "website" should help the frontend render the data correctly.
>
> Version table: Having an additional field for schema_version make sense. Adding
> an "applied_at" timestamp field makes sense, please add them.
>
> Answers to part D:
>
> TLS certificate and key parameters do not exist: Please add the parameters
> --tls-cert (-t) and --tls-key (-T) with the defaults pointing to the files
> server.crt and server.key in the current working directory.
> Also add --bind-address (-b) with a default to bind to all addresses.
>
> Also add --database-sslmode and a --max-connection-pool setting for limiting the
> maximum open/idle database connections as suggested.
>
> No password hashing algorithm: If Argon2id can be hashed inside the browser and
> checked inside the backend, use that for hashing the password. Otherwise use
> bcrypt and recommend the cost floor.
>
> No session mechanism: use JWT. Idle timeouts should be configurable with
> --idle-timeout (default 6h) and absolute timeouts with --absolute-timeout (7d)
>
> Specify CSRF protection, a reasonable CORS policy, security headers/CSP, and
> rate limiting on login. The application is run on internal networks with a low
> number of non-malicious users having access to it.
>
> API paths are prefixed with /api/v1), error responses are returned using the
> standard HTTP error codes containing a JSON object with an internal error number
> and a short human readable error message.
> As the number of items is usually low enough and the bandwidth in the local
> network is high, there is no need for pagination. If a menu is large, items will
> be grouped in categories.
> Filtering restaurants is not needed, filtering menu items can be done by
> selecting tags and Allergens.
> As every oser can only edit their own order item and only the creator of an
> order can make changes to the order itself, concurrent order edits should be
> rare. and restricting a user to one login at a time should solve the issue of
> one uswer accessing the same order from multiple browsers at the same time.
>
> Third-party API auth: Third party access is not the main focus for the first
> iteration of the application. Please specify a token authentication mechanism,
> where users can create and manage tokens used by that.
>
> No migration tooling named: Use golang-migrate for migration. and the "update"
> statement will be used for managing database schema updates after the initial
> release of the application. It will also be used to update the configuration
> file.
>
> No version verb or --version flag, despite a version page in the UI: Add a verb
> "version" that shows the last entry of the version table to the specification.
>
> Operational endpoints missing: Add a /api/v1/health endpoint detailing that
> returns a simple ok status, if the database connection is established.
> Add a /api/v1/metrics endpoint that provides the number of restaurants, menu
> items, open and expired orders, users and the number of database connections.
>
> Add an /api/v1/shutdown endpoint that shuts down the application. This endpoint
> can only be used by the admin user.
>
> HTTP read/write/idle timeouts, should be configurable, please add configuration
> parameters --http-read-timeout, --http-write-timeout, -http-idle-timeout with a
> default value of 300 seconds.
>
> Log rotation will be handled by logrotate, when using --log-file,
>
> Please specify adding request/correlation IDs in log lines.
>
> Retention for expired orders can be specified, using the --retention (-r)
> parameter. Add a verb "cleanup" that can be used by a cron job to periodically
> clean expired orders. In addition, the user who created an order or the
> administrator can remove it manually after it expired.
>
> PostgreSQL 18 will be the target version for the application. There is no need
> to support 16/17 as 18 has been released for almost a year now.
>
> Even though DEBUG "each function logs the function call with parameters" is
> extremely heavy, this never posed a problem in the past when using zerolog. It
> also helped pinpointing problems during development of other applications and
> should only be used under extreme circumstances.
>
> Answers to part E:
>
> Add a specification for the summary page. This page will be displayed by
> clicking on the summary button on the order page or a summary button integrated
> in the order tile on the order overview page.
>
> The application will mostly be used in web browsers on PCs running Windows /
> MAC. It should be responsive and useable on tablets and phones equally well but
> the strategy is not "mobile first" but "Firefox and Chrome first"
>
> There should be a Login/Register button in the title, when no user is logged in.
> When a user is logged in, it is replaced by the user name/handle and a logout
> symbol.
>
> There is no need for a "sharing mechanism", the app will be available in a
> company/club intranet and promoted through their channels.
>
> Having an "empty" state should always show only the tile for adding entries to
> the respective list. The application runs on a fast intranet/LAN environment,
> lazy loading, combined with a fast network should minimize the need for a
> "loading" state to the mechanism already provided by the browser and having a
> confirmation record for every destructive action together with restricting
> deletion of restaurants and menus to the administrator user and only allow the
> user who created an order and the admin to delete the order limits the impact
> there, too.
>
> No accessibility target: Please specify WCAG 2.1 AA and require keyboard
> navigation
>
> Language and locale precedence should be explicit: cookie -> Accept-Language ->
> English. Dates and times and numbers should be formatted per locale, too.
> However, the currency is tied to the restaurant and does not change based on
> nthe users language.
>
> No search or sort on restaurant/order overviews: As the number of restaurants
> usually used by the targeted audiences as well as the number of orders will be
> rather small (Expect between 3 and 10 restaurants and maybe 3 concurrent active
> orders per day), a search or filter functionality is currently not the focus for
> the application.
>
> Deadline notifications: That is currently not planned.
>
> The imprint page should be stored in a file as a HTML snippet. As the
> application is intended as an intranet tool and not publicly available, there is
> no legal requirement for it in that context. When logged in as an administrator,
> there should be a button visible to replace this content in the database. During
> the initial installation, the user should be asked for a file location for it.
>
> part F:
>
> A "legal notes" page should be created and handled in a similar matter to the
> imprint page, where the operator of the application can put the required
> information.
> Please add a document to the project where we specify what GDPR related and
> legal information
>
> part G:
>
> Please go ahead with your suggestion and restructure the documentation.
>
> part H:
>
> please act on all the typos and the broken sentence.

### Prompt 3

> Please add the requirement of using the
> https://github.com/julienschmidt/httprouter package for handling the API and
> stastic endpoints
>
> Reduce the http timeout default value to not kill the SSE streams.
>
> Modify feature F1.2. Users that are not logged in can't see the ordered items,
> only the number of order items
>
> Modify feature F1.3 that only logged in users who have participated in an order
> can see the summary page
>
> Modify feature F4.6 that items inside categories are also ordered by their
> restaurant specific ID and then name as this is usually the case on a menu
> anyway.
>
> In 03_data_model.md, the table currency is has name_en and name_de fields. The
> translation of the currency names should be managed by the i18n functionality
> of the frontend. I prefer this over adding more columns to the table whenever
> we add a translation.
>
> In 05_auth_and_permissions.md, please add the following password complexity
> rules:
> The password must satisfy at least 3 of the below rules:
> - Must contain at least one upper letter
> - Must contain at least one lower case letter
> - Must contain at least one digit
> - Must contain at least one special character out of
>   &lt;&gt;|-_.:,;#'!"§$%&/()[]{}?@
> - Must contain at least one language specific character, currenccy symbol etc.
>   for example äöüÄÖÜß€áàâéèêíìîóòôúùû and so on
>
> In 06_ui_ux.md Don't limit the languages in the language selector to German and
> English but rather to the list of available translations that are part of the
> application. The initial version will only provide those two languages but
> later iterations may add more languages.
>
> In 09_configuration.md, change the rule to warn if the configuration has another
> file mode than 0600 to a fatal error. Warnings can be ignored, fatal errors that
> prevent the application from starting must be fixed.
>
> In 10_operations.md: Add an alternative to step one in the installation routine
> that installs a .deb package on Debian or Ubuntu and a .rpm on Redhat
> derivatives. These packages will alo contain the cronjob definitions, a
> logrotate configuration and a systemd service.
> Also add a section to run the application with docker/docker compose.
>
> Related to 13_legal_and_privacy.md: Check the dependencies listed in
> 08_technologies.md, whether the doenerstag application can use the BSD 3-clause
> license and if so, add the license file.

### Prompt 4

> Please move the names for allergen and additive to the catalogs, too.

### Prompt 5

> Please create a list of implementation tasks that are grouped by sprints. Order
> these sprints and tasks so that we don't need to run multiple sprints in
> parallel. Focus on setting up the database and running the installer first

## Sprint 1

Prompts 1 to 5 above shaped the specification and the plan, and are the work on
`sprint-0`. From here on the prompts drive implementation, and each group is
recorded on the branch of the sprint it belongs to.

### Prompt 6

> Please implement the tasks from the first sprint in the sprint plan. Ideally
> tackle one task after another an finish every task by committing the changes.
> Use Task <task number>: <Task description> for the commit message. If some
> tasks must be executed in parallel or out of order, that is not a problem, as
> long as the tasks are referenced in the message. If it turns out, that
> additional tasks are required to complete the task description, add them to the
> list of tasks, using a 3rd level in the task id, e.g. 1.1.1 as an additional
> task required to reach the goal initially defined in 1.1. Mark completed tasks
> in the implementation plan.

## 2026-09-06

### Prompt 7

> I have created a file contrib/setup_dev_pipeline.sh that already installs
> postgresql-18 and docker.
> I have also set up a VM with the IP 192.168.178.183 andf the user doener on
> that machine. You can connect to that VM by ssh, relying on pageant and the
> already loaded keys or by explicitly using the passwordless SSH key
> /c/Users/white/.ssh/id_ed25519_claude. The user doener can run sudo without
> providing a password. All programs from the putty software stack are installed
> on this computer, you can either rely on the tools provided by VSCode or these
> programs to access the VM.
>
> Please add these additional tasks to Sprint 1:
> - Modify the contrib/setup_dev_pipeline.sh script to install all software
>   necessary to run the build and test pipeline at this stage.
> - Also add the installation of tools that we already know we need to the script.
> - Run the build and test pipeline on the Linux VM

### Prompt 8

> I have created the github repository and pushed both, the main and sprint-1
> branch. The CI pipeline ran green:
> https://github.com/joernott/doenerstag/actions/runs/34012252189.
>
> Please add another task to sprint 1:
> - When running the coverage tests,, preserve both results from Windows and
>   Linux as <OS>-coverage.out and run  go tool cover -html=<OS>coverage.out -o
>   <OS>coverage.html

## Sprint 2

### Prompt 9

> Please push the sprint-1 branch and continue implementation with Sprint2

### Prompt 10

> please create a sprint-0 branch from the last commit before the first commit
> belonging to sprint 1. Please rename the curent sprint-1 branch as sprint-2 and
> create a new sprint-1 branch that only contain all the commits belonging to
> sprint 1. In the future, always create a sprint-<number> branch for every
> sprint.

### Prompt 11

> govulncheck fails in the CI:
> https://github.com/joernott/doenerstag/actions/runs/34015733078/job/101439063700
> The  Test (Windows)  check also fails in the CI:
> https://github.com/joernott/doenerstag/actions/runs/34015733078/job/101439063710
>
> Create an additional task in sprint 2 to fix those CI errors and execute that
> task

### Prompt 12

> The file docs/zz_prompts.md seems to be missing the last few prompts. Please
> add the prompts belonging to sprint 1 to the sprint-1 branch, merge that into
> sprint-2 and then add the prompts belonging to sprint 2 there as well. I'd like
> to keep a history of all prompts in that file to document the process.

### Prompt 13

> The windows test fails, reporting the error
>
> ```
> # .out
> no required module provides package .out; to add it:
>         go get .out
> FAIL    .out [setup failed]
> ```
>
> when running `go test -coverprofile=windows-coverage.out ./...`

### Prompt 14

> The CI task ran successfully:
> https://github.com/joernott/doenerstag/actions/runs/34016806972

---

## Sprint 3

### Prompt 15

> Please continue with sprint 3

### Prompt 16

> Please continue

### Prompt 17

> please push sprint-3 and check, if the CI runs successfully. If that is the
> case, please continue with the remaining tasks. If a CI run breaks, you can
> always ask me to point you to the web page containing the logs.

### Prompt 18

> Please continue with the remaining steps

---

## Sprint 4

### Prompt 19

> Please update the documentation to state that there are now five
> forbidden-on-the-command-line settings. Then continue with the implementation
> of sprint 4.

### Prompt 20

> yes, please continue with the remaining steps

### Prompt 21

> Please build and run the application on the VM. I want to access is myself
> using my browser

---

## Sprint 5

### Prompt 22

> I have tested the above endpoints and things work as expected. You can stop
> the application and start implementing sprint 5

---

## Sprint 6

### Prompt 23

> Please start with implementing sprint 6
