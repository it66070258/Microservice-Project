CREATE TABLE IF NOT EXISTS "Course" (
	"course_id" INTEGER NOT NULL UNIQUE,
	"subject" VARCHAR(255) NOT NULL,
	"credit" INTEGER NOT NULL,
	"section" VARCHAR(255) ARRAY NOT NULL,
	"day_of_week" VARCHAR(255) NOT NULL,
	"start_time" TIME NOT NULL,
	"end_time" TIME NOT NULL,
	"capacity" INTEGER NOT NULL,
	"state" VARCHAR(255) NOT NULL,
	"current_student" VARCHAR(255) ARRAY,
	"prerequisite" VARCHAR(255) ARRAY,
	PRIMARY KEY("course_id")
);
