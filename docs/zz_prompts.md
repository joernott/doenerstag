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

---

## Sprint 7

### Prompt 24

> Please continue with sprint 7

---

## Sprint 8

### Prompt 25

> Please continue with sprint 8

### Prompt 26

> Please change the documentation and implementation so, that the creator
> display name is not shown to anonymous requests. Also, the document
> 14_implementation_plan.md does not show checkmarks for tasks 8.1 to 8.9.
> Please ensure, that bthese tasks have been completed and are marked as
> completed.

---

## Sprint 9

### Prompt 27

> Please continue with sprint 9

---

## Sprint 10

### Prompt 28

> Please start sprint 10

---

## Sprint 11

### Prompt 29

> Please start the application on the VM, so I can haver a first look

### Prompt 30

> I am done, please stop the application and continue with sprint 11

---

## Sprint 12

### Prompt 31

> please continue with sprint 12

---

## Sprint 13

### Prompt 32

> Please start sprint 13

---

## Sprint 14

### Prompt 33

> please start implementing sprint 14

---

### Prompt 34

> please continue

---

### Prompt 35

> I have mounted the doenerstag folder into the VM as /home/doener/doenerstag,
> so you don't need to copy any content of the folder to the VM. Please start
> the application in the VM using the static folder and not the embedded one.

---

### Prompt 36

> Using the dark schema, the orange buttons turn black when hovering over them.
> That makes the black text unreadable. In light mode, they turn white. That
> makes the light gray text almost unreadable. When hovering, the text should
> turn orange (the same color as the button itself held before). That way we
> still have enough contrast.

---

### Prompt 37

> More modifications:
> Global:
> - I placed two SVG graphics (doenerstag_dark and doenerstag_bright) into
>   contrib. Please use them as logo in the title bar. You can move them to the
>   appropriate place. As they only differ in the color of the border and text,
>   you might be able to use only one of them if you can make the color
>   customizable. The size of the logo should not change too much
> - In the tile used to add new orders and new restaurants, make the + sign
>   approximately 75% the size of the tile
> - If possible, show a very faded version of the doenerstag logo centered and
>   scaled up to 80% of the height on the background of the overview pages
> On the order overview page:
> - there is a summary button, partially hidden behind the tiles. It points to
>   the ordeer Pinar_mtq94gr8338 where I added one entry myself. That button
>   should only be visible when showing the order but on that page, it is not
>   visible at all.
> - remvove the number of positions and participants as well as the total amount
>   from the tiles on the overview page as well as the creator
> - Add a "Summary" button inside each tile and if the creator is the currently
>   logged in user, also show an edit button (only using a pencil) and a delete
>   button, only using a trashcan icon
> - If a restaurant has no logo, show the doenerstag logo instead
> On the restaurant overview page:
> - If a restaurant has no logo, show the doenerstag logo instead
> On the restaurant page:
> - Use a tabbed design instead of showing tiles in a long scroll list. The tab
>   "Menu" should be the first tab when showing an already existing restaurant.
> - Add a red delete button on the same height as the title. but aligned to the
>   right border of the tile containing the tabs. If a restaurant can't be
>   deleted, fade it.
> - Move the save button to the same height as the restaurant title, left of the
>   delete button Fade it as long as there are no changes to the restaurant data
> - Use a more compact design for the opening hours: The dropdowns should be max
>   10% larger than the largest text. Always reserve space for the "past
>   midnight" note to achieve a more uniform design.
> - The remove button for tjhe opening hours should be red like the other remove
>   buttons

---

### Prompt 38

> On the order page:
> - Move the Summary button outside the first tile, on nthe same level as the
>   title, align it to the right and make it orange
> On the restaurant page:
> - The title of every tab is mirrored as heading in the tab. That is redundant
>   and should be removed
> - The dropdown and input fields for a new contact should have the same size as
>   the ones for the existing contact entries. The dropdown should also be not
>   too much longer than the longest entry.
> - Add a button with an icon behind the value field of a contact, the icon
>   should match the type of contact. The link should open in a new tab where it
>   makes sense. For example, any phone type entry (telephone,. mobile) should
>   use a tel: URL using the value, Use a similar logic to fax, email and
>   website. Address should open google maps at the given address. If the type is
>   "Other" don't show the button but still reserve the link
> - Move the remove button on the "opening hours" tab right behind the "past
>   midnight" block.
> - I changed the name of the
>   https://192.168.178.183:8443/restaurants/01a0765f-517b-748a-bdb0-c4495513ae94
>   restaurant and saved it. The title did not change immediately, only after a
>   reload. That change should trigger immediately.
> - On the "Menu" tab, move the "Add item" button to the top and make it orange,
>   Add a "Add category" button next to it and move the "Add Category"
>   functionality at the bottom of the tab to a modal dialog
> - In tzhe menu tap, the category shoul be rendered as a heading instead of an
>   input field. There should be an icon left to it that shows a down or sideways
>   arrow to show and hide the elements in that category. Replace the "Save"
>   button with an orange "Edit" button

---

### Prompt 39

> Having the day dropdown at 118px is fine with me
> More changes to the restaurant page:
> - On the menu tab, make the Edit button for menu items orange as well. Align
>   the edit and the up/down buttons for categories to the right as well and add
>   a border around the category headline and buttons
> - On the restaurant tab: Make the "Remove the picture" button red and the
>   "Replace picture" button orange
> - On the contact page, make the "Save" buttons and "Add a contact" button
>   orange, resize the "Adfd a contact" button so that it's left border aligns
>   with the "Save " buttons above and the right border aligns with the "Remove"
>   button.
> - Remove the "Save" button from the "Opening hours" tab and make the "Add
>   opening hours" button orange
> On the order page:
> - Increase the distance between the Total and the red warning box "Below the
>   minimum value", so that the distance between the border and the Total is at
>   least as big as the distance to the text inside the box
> - Make the "Add an item" page on the menu there orange as well
> - Always show the "Summary" button on the order but fade it out if the order
>   can't be placed
> On the order overview: Always show the "Summarty" button but fade it out if the
> order can't be placed
> On the account page:
> - Use the same tab design as for the restaurant.
> - Move the "Delete my account" button to the top on the level of the Heading
>   and align it to the right.
> - Move the "Save" button to the left of the delete account. Fade it out if
>   there are no changes to be saved
> - Remove the second "Save" button on the password tile, that functionality
>   should be covered by the button in the top line
> - Remove the warning that an account deletion can't be uindone and get ridf of
>   the whole tile. The warning cayn be shown in a modal after clicking the
>   button.
> On the version page:
> - Add the background image similar to the overview pages and create a tile
>   around the version information
> Open the api documentation in a separate tab

---

### Prompt 40

> On the order page:
> - After adding my own order item, the Summary button is still locked. Make
>   sure, it is unlocked when I place an order and make sure, it is locked again,
>   when I remove my last order item
> - I just created a new order. There should be an "Edit" button to the right of
>   the "Summary" button and the "Delete order" button should be to the right of
>   that one
> - Also, creating an order where the deadline is before the creation date should
>   be blocked with an error message. My newly created order
>   https://192.168.178.183:8443/orders/01a07d8b-ff37-7ccd-8dc1-bd23c5824b6e has
>   a deadline 11 hours ago

---

### Prompt 41

> On the Summary page:
> - Increase the distance between the Total line and the "Below the minimum
>   order" box similarly to what you've done on the order page itself.

---

### Prompt 42

> Also, when creating a new order, the default value for deadline should be 1
> hour in the future and the pickup/delivery time 2 hours in the future

---

### Prompt 43

> I just added to items to the order
> https://192.168.178.183:8443/orders/01a07d13-d904-7719-8b00-de83c0aac359. This
> did not unlock the summary button on that page.

---

## Sprint 15

### Prompt 44

> Please start with sprint 15 but don't tag that version as 1.0.0 but rather
> 0.1.0 as this is the first iteration providing the minimal functionality and
> has barely any user testing done. Add a final task to that sprint that creates
> a github release for version 0.1.0. That release should contain the following
> additional artifacts: the Windows executable in a .zip file, the Linux
> executable inside a tar.gz, the .rpm and .deb file as well as the Dockerfile.
> Add another task to push the docker image to docker.io/joernott/doenerstag,
> using the username joernott and the access token "[REDACTED — see the note in
> docs/10_operations.md; the value was supplied in chat and is not stored in
> this repository]" I created for you. Please censor the token when you store
> the prompt in the document zz_prompts.md

---

### Prompt 45

> I have addes the two secrets. Please squash merge all sprint branches up to
> sprint-14 in ascending order into the main branch but without removing these
> branches from the repository.
> To test the installation of the RPM package, you can use something like
>
> ```yaml
> jobs:
>   rpm-job:
>     # Uses the free standard Ubuntu runner
>     runs-on: ubuntu-latest
>
>     # Forces the job to run inside a Fedora environment
>     container:
>       image: fedora:latest
>     steps:
>       - name: install RPM
>         run: |
>           rpm -ivh doenerstag-$VERSION.rpm
> ```
>
> Please add tasks in sprint 15 to test the package building and installation as
> well as building and running the docker container in the ci and the release
> workflow.
> After that, you can squash-merge the sprint-15 baranch into main as well and
> tag the version v0.1.0 there

---

## Sprint 16

### Prompt 46

> Start a branch for sprint 16 from main. This sprint will be used mainly for
> bug-fixing and minor improvements.
> The first task in that is related to a bug: Yesterday, I did not log out from
> the web UI on the server. Now, I am getting a json response with error 2003,
> wzhenever I try to access https://192.168.178.183:8443/ from that browser. The
> expected behaviour would be to either show an error page that allows a relogin
> or show the main page as anonymous user with a modal dialog that I have been
> logged out.

---

### Prompt 47

> Some more tasks:
> - Add the installation of mokapi to the setup_dev_pipeline.sh script and
>   install it on the VM. Also maske sure, it is configured for Mail (SMTP and
>   IMAP) and LDAP. This can be used to test the email functionality.
> - Add support for sending EMails to the application and add a "Forgot
>   password?" option in the login dialog beind the Login button. That should
>   generate a unique reset-password ID, that is kept in memory for one hour. It
>   should also trigger sending an email to the email address of the user. The
>   email should contain a link to a password reset page with the unique ID and
>   allow the user to reset their password. After completing and submitting the
>   form, the user should be redirected to the login page.
> - Add the new verb "user" with the following subcommands
>   - list lists all users with their id
>   - add adds a user with the following parameters --username (-u),
>     --displayname (-d), --email (-e): That command should generate a 20
>     character password using the compleyity rules, set it and print it out
>   - delete deletes a user, either --id (-i) or --username (-u) must be provided
>     to identify the user
>   - password resets the password for a user, either --id (-i) or --username
>     (-u) must be provided to identify the user. If --set-pasword is provided,
>     the application will ask for the new password, otherwise it will generate a
>     reset password link for the user
> - For the administrator user, there should be a menu entry "Users" that shows a
>   list of users with an edit, reset password and delete button for each user.
>   The edit button should open the user dialog for the respective user, the
>   reset password button should trigger the same password reset functionality as
>   if the user had clicked on the "reset password" link on the login page.
> - Add a new verb "restaurant that has the following subcommands
>   - list lists all restaurants with their ID
>   - delete deletes a restaurant and depending menu items, orders, opening hours
>     etc.,  --id (-i) must be specified
>   - export exports a restaurant with opening hours, contacts menus and
>     associated tags. --id (-i) must be provided, --format (-F) specifies the
>     file format. It can either be yaml or json. If -i is provided multiple
>     times, multipüle restaurants are exported. if --all (-a) is specified, all
>     restaurants are exported.
>   - import imports a restaurant, --file (-f) specifies the file, the
>     application tries to guess the format by looking at the content, not the
>     file suffix, this can be overridden by providing the --format (-F)
>     parameter. A file can contain multiple restaurants. If a restaurant with the
>     given uuid already exists, it will hnot be imported. this can be overridenn
>     by providing the --overwrite (-o) option. In that case, the existing
>     restaurant with that ID is deleted before the new restaurant is imported

---

### Prompt 48

> Some more tasks for this sprint:
> 1. Move --log-file to -L please
> 2. Also add another verb "order" with the following subcommands:
> - list tto list all orders with their ID, the option --verbose (-V) also lists
>   creator, restaurant name and ID, deadline, pickup/delivery and the time for
>   that as well as the number of order items
> - delete deletes an order, the parameter --id (-i) must be provided
> 3. Please also add a doc page with all the commandline verbs and their
>    parameters, essentially, what you get when using --help
> 4. Add a cleanup job to the beginning of the local testing that deletes all
>    orders, restaurants and users from previous jobs. Having one set of test data
>    at the end of it is fine, but the database is getting crowded
> 5. On the orders overview add a "cleanup" button on the same height as "orders",
>    aligned to the right. It should only be visible to the administrator user
>
> After that, please make sure, the development version is deployed on the VM.
> Currently, calling "doenerstag" is resolved as /usr/local/bin/doenerstag and
> that is 3 days old and does not have any of the new verbs.

---

### Prompt 49

> The table for sprint 16 in 14_implementation_plan.md is broken starting with
> task 16.8, please fix that. Also make sure to only delete the
> users/restaurants/orders you created during previous tests, you should be able
> to determine that by the user name. You also deleted the user I created in the
> current cleanup.

---

### Prompt 50

> Another task for the sprint. Currently, the binary ends up in /usr/local/bin.
> Make sure that it is in /usr/bin in the RPM and debian package

---

### Prompt 51

> I have added 4 pictures to the contrib folder. They contain the memory for the
> restaurant "Ali Baba". On the regular pages (black), the categories are
> highlighted in red and the menu is white on black, on the getraenke page, the
> background is red, so the categories are highlighted in black. Can you analyze
> these four pictures and create a json import file in the contrib folder to use
> with doenerstag restaurant import?

---

### Prompt 52

> I've restarted the VM. Can you restart the application and reset the root
> password with a random password and then post that password here?

---

### Prompt 53

> On the order page, there is a + and x for showing/hiding categories. Please use
> the same symbols, you use on the restaurant page for the categories on the menu
> tab.

---

### Prompt 54

> Another task: When someone clicks on the login link on any page, he should
> return there. Only when registering, they should end up on their account page
> after clicking on "register"
>
> I have modified the data for the restaurant Ali Baba, please update the export
> file in the contrib folder and remove the images from the folder as well.
